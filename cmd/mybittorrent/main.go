package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
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

	decodeCmd := flag.NewFlagSet("decode", flag.ExitOnError)
	infoCmd := flag.NewFlagSet("info", flag.ExitOnError)
	peersCmd := flag.NewFlagSet("peers", flag.ExitOnError)
	handshakeCmd := flag.NewFlagSet("handshake", flag.ExitOnError)
	downloadPieceCmd := flag.NewFlagSet("download_piece", flag.ExitOnError)
	downloadCmd := flag.NewFlagSet("download", flag.ExitOnError)
	magnetParseCmd := flag.NewFlagSet("magnet_parse", flag.ExitOnError)
	magnetHandshakeCmd := flag.NewFlagSet("magnet_handshake", flag.ExitOnError)

	downloadPieceOutput := downloadPieceCmd.String("o", "", "output file path")
	downloadOutput := downloadCmd.String("o", "", "output file path")

	if len(os.Args) < 2 {
		logger.Error("Expected subcommand")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "decode":
		err = decodeCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse decode command", zap.Error(err))
			os.Exit(1)
		}
		err = handleDecode(decodeCmd.Args())

	case "info":
		err = infoCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse info command", zap.Error(err))
			os.Exit(1)
		}
		err = handleInfo(infoCmd.Args())

	case "peers":
		err = peersCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse peers command", zap.Error(err))
			os.Exit(1)
		}
		err = handlePeers(peersCmd.Args())

	case "handshake":
		err = handshakeCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse handshake command", zap.Error(err))
			os.Exit(1)
		}
		err = handleHandshake(handshakeCmd.Args())

	case "download_piece":
		err = downloadPieceCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse download_piece command", zap.Error(err))
			os.Exit(1)
		}
		err = handleDownloadPiece(*downloadPieceOutput, downloadPieceCmd.Args())

	case "download":
		err = downloadCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse download command", zap.Error(err))
			os.Exit(1)
		}
		err = handleDownload(*downloadOutput, downloadCmd.Args())

	case "magnet_parse":
		err = magnetParseCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse magnet_parse command", zap.Error(err))
			os.Exit(1)
		}
		err = handleMagnetParse(magnetParseCmd.Args())

	case "magnet_handshake":
		err = magnetHandshakeCmd.Parse(os.Args[2:])
		if err != nil {
			logger.Error("Failed to parse magnet_handshake command", zap.Error(err))
			os.Exit(1)
		}
		err = handleMagnetHandshake(magnetHandshakeCmd.Args())

	default:
		logger.Error("Unknown command", zap.String("command", os.Args[1]))
		os.Exit(1)
	}

	if err != nil {
		logger.Error("Command failed",
			zap.String("command", os.Args[1]),
			zap.Error(err),
			zap.Strings("args", os.Args[2:]))
		os.Exit(1)
	}
}

func handleDecode(args []string) error {
	bencodedValue := args[0]
	decoded, _, err := bencode.Decode[any](bencodedValue)
	if err != nil {
		return err
	}
	jsonOutput, _ := json.Marshal(decoded)
	fmt.Println(string(jsonOutput))
	return nil
}

func handleInfo(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("file path required")
	}
	filePath := args[0]

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	info, err := bencode.Info(string(fileContent))
	if err != nil {
		return fmt.Errorf("failed to decode file content: %w", err)
	}

	hash, _, err := bencode.HashInfo(info)
	if err != nil {
		return fmt.Errorf("failed to encode info: %w", err)
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
	if len(args) < 1 {
		return fmt.Errorf("file path required")
	}
	filePath := args[0]

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	info, err := bencode.Info(string(fileContent))
	if err != nil {
		return fmt.Errorf("failed to decode file content: %w", err)
	}

	peers, err := peering.GetPeers(info)
	if err != nil {
		return fmt.Errorf("failed to get peers: %w", err)
	}

	for _, peer := range peers {
		fmt.Printf("%s:%d\n", peer.IP, peer.Port)
	}
	return nil
}

func handleDownloadPiece(outputPath string, args []string) error {
	if outputPath == "" || len(args) < 2 {
		return fmt.Errorf("usage: download_piece -o <output-path> <torrent-file> <piece-index>")
	}

	torrentPath := args[0]
	pieceIndex, err := strconv.Atoi(args[1])
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

func handleDownload(outputPath string, args []string) error {
	if outputPath == "" || len(args) < 1 {
		return fmt.Errorf("usage: download -o <output-path> <torrent-file>")
	}

	torrentPath := args[0]

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
	if len(args) < 2 {
		return fmt.Errorf("not enough arguments. Usage: handshake <torrent-file> <peer-address>")
	}

	torrentPath := args[0]
	peerAddr := args[1]

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
	if len(args) < 1 {
		return fmt.Errorf("usage: magnet_parse <magnet-link>")
	}

	magnetLink := args[0]
	link, err := magnet.Parse(magnetLink)
	if err != nil {
		return fmt.Errorf("failed to parse magnet link: %w", err)
	}

	if len(link.Trackers) == 0 {
		return fmt.Errorf("no trackers found in magnet link")
	}

	fmt.Printf("Tracker URL: %s\n", link.Trackers[0])
	fmt.Printf("Info Hash: %s\n", link.InfoHash)

	return nil
}

func handleMagnetHandshake(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: magnet_handshake <magnet-link>")
	}

	magnetLink := args[0]
	link, err := magnet.Parse(magnetLink)
	if err != nil {
		return fmt.Errorf("failed to parse magnet link: %w", err)
	}

	// Convert hex info hash to bytes
	infoHash, err := hex.DecodeString(link.InfoHash)
	if err != nil {
		return fmt.Errorf("failed to decode info hash: %w", err)
	}

	// Get peers from tracker
	trackerURL := link.Trackers[0]
	peers, err := peering.GetPeersFromTracker(trackerURL, infoHash)
	if err != nil {
		return fmt.Errorf("failed to get peers: %w", err)
	}

	if len(peers) == 0 {
		return fmt.Errorf("no peers available")
	}

	// Connect to first peer
	peer := peers[0]
	peerAddr := fmt.Sprintf("%s:%d", peer.IP, peer.Port)
	conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %w", err)
	}
	defer conn.Close()

	// Perform handshake with extension bit set
	response, err := peering.PerformHandshake(conn, infoHash)
	if err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Extract and print peer ID
	peerID := response[48:68]
	fmt.Printf("Peer ID: %x\n", peerID)

	return nil
}
