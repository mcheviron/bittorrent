package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/bencode"
	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/peering"
	"go.uber.org/zap"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

func init() {
	var err error
	logger, err := zap.NewDevelopment()
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
			os.Exit(1)
		}
	case "info":
		if err := handleInfo(os.Args); err != nil {
			os.Exit(1)
		}
	case "peers":
		if err := handlePeers(os.Args); err != nil {
			os.Exit(1)
		}
	case "handshake":
		if err := handleHandshake(os.Args); err != nil {
			os.Exit(1)
		}
	default:
		logger.Error("Unknown command", zap.String("command", command))
		os.Exit(1)
	}
}
func handleDecode(args []string) error {
	bencodedValue := args[2]

	decoded, _, err := bencode.Decode(bencodedValue)
	if err != nil {
		fmt.Println(err)
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

	_, infoHash, err := bencode.HashInfo(info)
	if err != nil {
		logger.Error("Failed to get info hash", zap.Error(err))
		return err
	}

	trackerReq := &peering.TrackerRequest{
		InfoHash:   infoHash,
		PeerID:     peering.PeerID,
		Port:       6881,
		Uploaded:   0,
		Downloaded: 0,
		Left:       info.Info.Length,
		Compact:    1,
	}
	trackerURL := fmt.Sprintf("%s?%s",
		info.Announce,
		url.Values{
			"info_hash":  []string{string(trackerReq.InfoHash)},
			"peer_id":    []string{trackerReq.PeerID},
			"port":       []string{strconv.Itoa(trackerReq.Port)},
			"uploaded":   []string{strconv.Itoa(trackerReq.Uploaded)},
			"downloaded": []string{strconv.Itoa(trackerReq.Downloaded)},
			"left":       []string{strconv.Itoa(trackerReq.Left)},
			"compact":    []string{strconv.Itoa(trackerReq.Compact)},
		}.Encode())

	resp, err := http.Get(trackerURL)
	if err != nil {
		logger.Error("Failed to get tracker response", zap.Error(err))
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("Failed to read response body", zap.Error(err))
		return err
	}

	decoded, _, err := bencode.Decode(string(body))
	if err != nil {
		logger.Error("Failed to decode tracker response", zap.Error(err))
		return err
	}

	trackerResp, ok := decoded.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid tracker response format")
	}

	peers := peering.ParsePeers(trackerResp["peers"].(string))
	for _, peer := range peers {
		fmt.Printf("%s:%d\n", peer.IP, peer.Port)
	}

	return nil
}

func handleHandshake(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("not enough arguments. Usage: handshake <torrent-file> <peer-address>")
	}

	torrentPath := args[2]
	peerAddr := args[3]

	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		return fmt.Errorf("failed to read torrent file: %v", err)
	}

	info, err := bencode.Info(string(torrentData))
	if err != nil {
		return fmt.Errorf("failed to parse torrent file: %v", err)
	}

	_, infoHash, err := bencode.HashInfo(info)
	if err != nil {
		return fmt.Errorf("failed to compute info hash: %v", err)
	}

	conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %v", err)
	}
	defer conn.Close()

	// handshake message
	handshake := make([]byte, 0, 68)
	handshake = append(handshake, byte(19))
	handshake = append(handshake, []byte("BitTorrent protocol")...)
	handshake = append(handshake, make([]byte, 8)...) // reserved bytes
	handshake = append(handshake, infoHash...)
	handshake = append(handshake, []byte(peering.PeerID)...)

	if _, err := conn.Write(handshake); err != nil {
		return fmt.Errorf("failed to send handshake: %v", err)
	}

	// recv
	response := make([]byte, 68)
	if _, err := io.ReadFull(conn, response); err != nil {
		return fmt.Errorf("failed to receive handshake: %v", err)
	}

	// verify
	if response[0] != 19 || string(response[1:20]) != "BitTorrent protocol" {
		return fmt.Errorf("invalid handshake response")
	}

	// extract peer id
	responsePeerID := response[48:68]
	fmt.Printf("Peer ID: %x\n", responsePeerID)

	return nil
}
