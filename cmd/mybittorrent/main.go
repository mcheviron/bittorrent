package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/bencode"
	"go.uber.org/zap"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

type TrackerRequest struct {
	InfoHash   []byte `json:"info_hash"`
	PeerID     string `json:"peer_id"`
	Port       int    `json:"port"`
	Uploaded   int    `json:"uploaded"`
	Downloaded int    `json:"downloaded"`
	Left       int    `json:"left"`
	Compact    int    `json:"compact"`
}
type TrackerResponse struct {
	Interval int    `json:"interval"`
	Peers    string `json:"peers"`
}

type Peer struct {
	IP   net.IP
	Port uint16
}

const peerID = "-MY0001-123456789012"

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

	trackerReq := &TrackerRequest{
		InfoHash:   infoHash,
		PeerID:     peerID,
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

	peers := parsePeers(trackerResp["peers"].(string))
	for _, peer := range peers {
		fmt.Printf("%s:%d\n", peer.IP, peer.Port)
	}

	return nil
}

func parsePeers(peersData string) []Peer {
	peers := make([]Peer, 0, len(peersData)/6)

	for i := 0; i < len(peersData); i += 6 {
		peer := Peer{
			IP:   net.IP([]byte(peersData[i : i+4])),
			Port: binary.BigEndian.Uint16([]byte(peersData[i+4 : i+6])),
		}
		peers = append(peers, peer)
	}

	return peers
}
