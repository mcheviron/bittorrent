package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/bencode"
	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/magnet"
	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/peering"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func init() {
	var err error
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(logger)
}

func main() {
	logger := zap.L()
	command := os.Args[1]

	switch command {
	case "decode":
		if err := handleDecode(os.Args); err != nil {
			logger.Error("Failed to decode", zap.Error(err))
			os.Exit(1)
		}
	case "info":
		if err := handleInfo(os.Args); err != nil {
			logger.Error("Failed to get info", zap.Error(err))
			os.Exit(1)
		}
	case "peers":
		if err := handlePeers(os.Args); err != nil {
			logger.Error("Failed to get peers", zap.Error(err))
			os.Exit(1)
		}
	case "handshake":
		if err := handleHandshake(os.Args); err != nil {
			logger.Error("Failed to handshake", zap.Error(err))
			os.Exit(1)
		}
	case "download_piece":
		if err := handleDownloadPiece(os.Args); err != nil {
			logger.Error("Failed to download piece", zap.Error(err))
			os.Exit(1)
		}
	case "download":
		if err := handleDownload(os.Args); err != nil {
			logger.Error("Failed to download", zap.Error(err))
			os.Exit(1)
		}
	case "magnet_parse":
		if err := handleMagnetParse(os.Args); err != nil {
			logger.Error("Failed to parse magnet link", zap.Error(err))
			os.Exit(1)
		}
	default:
		logger.Error("Unknown command", zap.String("command", command))
		os.Exit(1)
	}
}

// Command handlers

func handleDecode(args []string) error {
	bencodedValue := args[2]
	decoded, _, err := bencode.Decode[any](bencodedValue)
	if err != nil {
		return err
	}
	jsonOutput, _ := json.Marshal(decoded)
	fmt.Println(string(jsonOutput))
	return nil
}

func handleInfo(args []string) error {
	logger := zap.L()
	if len(args) < 3 {
		logger.Error("File path is required for info command")
		return fmt.Errorf("file path required")
	}
	filePath := args[2]

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		logger.Error("Failed to read file", zap.Error(err))
		return err
	}

	info, err := bencode.Info(string(fileContent))
	if err != nil {
		logger.Error("Failed to decode file content", zap.Error(err))
		return err
	}

	hash, _, err := bencode.HashInfo(info)
	if err != nil {
		logger.Error("Failed to encode info", zap.Error(err))
		return err
	}

	fmt.Printf("Tracker URL: %s\n", info.Announce)
	fmt.Printf("Length: %d\n", info.Info.Length)
	fmt.Printf("Info Hash: %s\n", hash)
	fmt.Printf("Piece Length: %d\n", info.Info.PieceLength)
	fmt.Println("Piece Hashes:")

	pieces := info.Info.Pieces
	for i := 0; i < len(pieces); i += 20 {
		pieceHash := pieces[i : i+20]
		fmt.Printf("%x\n", pieceHash)
	}
	return nil
}

func handlePeers(args []string) error {
	logger := zap.L()
	if len(args) < 3 {
		logger.Error("File path is required for peers command")
		return fmt.Errorf("file path required")
	}
	filePath := args[2]

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		logger.Error("Failed to read file", zap.Error(err))
		return err
	}

	info, err := bencode.Info(string(fileContent))
	if err != nil {
		logger.Error("Failed to decode file content", zap.Error(err))
		return err
	}

	peers, err := peering.GetPeers(info)
	if err != nil {
		logger.Error("Failed to get peers", zap.Error(err))
		return err
	}

	for _, peer := range peers {
		fmt.Printf("%s:%d\n", peer.IP, peer.Port)
	}

	return nil
}

func handleDownloadPiece(args []string) error {
	if len(args) != 6 {
		return fmt.Errorf("usage: download_piece -o <output-path> <torrent-file> <piece-index>")
	}

	if args[2] != "-o" {
		return fmt.Errorf("expected -o flag, got: %s", args[2])
	}
	outputPath := args[3]
	torrentPath := args[4]

	pieceIndex, err := strconv.Atoi(args[5])
	if err != nil {
		return fmt.Errorf("invalid piece index: %v", err)
	}

	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		return fmt.Errorf("failed to read torrent file: %v", err)
	}

	info, err := bencode.Info(string(torrentData))
	if err != nil {
		return fmt.Errorf("failed to parse torrent file: %v", err)
	}

	client, err := peering.NewClient(info)
	if err != nil {
		return err
	}

	pieceData, err := client.DownloadPiece(pieceIndex)
	if err != nil {
		return err
	}

	return os.WriteFile(outputPath, pieceData, 0644)
}

func handleDownload(args []string) error {
	if len(args) != 5 {
		return fmt.Errorf("usage: download -o <output-path> <torrent-file>")
	}
	if args[2] != "-o" {
		return fmt.Errorf("expected -o flag, got: %s", args[2])
	}
	outputPath := args[3]
	torrentPath := args[4]

	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		return fmt.Errorf("failed to read torrent file: %v", err)
	}

	info, err := bencode.Info(string(torrentData))
	if err != nil {
		return fmt.Errorf("failed to parse torrent file: %v", err)
	}

	client, err := peering.NewClient(info)
	if err != nil {
		return err
	}

	fileData, err := client.DownloadAll()
	if err != nil {
		return err
	}

	return os.WriteFile(outputPath, fileData, 0644)
}

func handleHandshake(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("not enough arguments. Usage: handshake <torrent-file> <peer-address>")
	}

	torrentPath := args[2]
	peerAddr := args[3]

	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		return fmt.Errorf("failed to read torrent file: %w", err)
	}

	info, err := bencode.Info(string(torrentData))
	if err != nil {
		return fmt.Errorf("failed to parse torrent file: %w", err)
	}

	conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %w", err)
	}
	defer conn.Close()

	_, infoHash, err := bencode.HashInfo(info)
	if err != nil {
		return fmt.Errorf("failed to calculate info hash: %w", err)
	}

	response, err := peering.PerformHandshake(conn, infoHash)
	if err != nil {
		return err
	}

	responsePeerID := response[48:68]
	fmt.Printf("Peer ID: %x\n", responsePeerID)

	return nil
}

func handleMagnetParse(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: magnet_parse <magnet-link>")
	}

	magnetLink := args[2]
	link, err := magnet.Parse(magnetLink)
	if err != nil {
		return fmt.Errorf("failed to parse magnet link: %w", err)
	}

	// At least one tracker is required
	if len(link.Trackers) == 0 {
		return fmt.Errorf("no trackers found in magnet link")
	}

	fmt.Printf("Tracker URL: %s\n", link.Trackers[0])
	fmt.Printf("Info Hash: %s\n", link.InfoHash)

	return nil
}
