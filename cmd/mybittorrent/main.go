package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
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
	"go.uber.org/zap/zapcore"
)

type message struct {
	Length  uint32
	ID      byte
	Payload []byte
}

type block struct {
	Begin  int
	Length int
}

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
	case "download_piece":
		if err := handleDownloadPiece(os.Args); err != nil {
			os.Exit(1)
		}
	default:
		logger.Error("Unknown command", zap.String("command", command))
		os.Exit(1)
	}
}

func handleDecode(args []string) error {
	bencodedValue := args[2]

	decoded, _, err := bencode.Decode[any](bencodedValue)
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

	peers, err := getPeers(info)
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
	logger := zap.L().Named("download_piece")
	logger.Info("Starting piece download",
		zap.Strings("args", args),
		zap.String("command", "download_piece"))

	if len(args) != 6 {
		logger.Error("Invalid number of arguments",
			zap.Int("expected", 6),
			zap.Int("got", len(args)),
			zap.Strings("provided_args", args))
		return fmt.Errorf("usage: download_piece -o <output-path> <torrent-file> <piece-index>")
	}

	if args[2] != "-o" {
		logger.Error("Missing -o flag",
			zap.String("got", args[2]),
			zap.Strings("all_args", args))
		return fmt.Errorf("expected -o flag, got: %s", args[2])
	}
	outputPath := args[3]
	torrentPath := args[4]

	pieceIndex, err := strconv.Atoi(args[5])
	if err != nil {
		logger.Error("Invalid piece index",
			zap.String("value", args[5]),
			zap.String("torrent_path", torrentPath),
			zap.String("output_path", outputPath),
			zap.Error(err))
		return fmt.Errorf("invalid piece index: %v", err)
	}

	logger.Info("Reading torrent file",
		zap.String("path", torrentPath),
		zap.String("output", outputPath),
		zap.Int("piece_index", pieceIndex))
	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		logger.Error("Failed to read torrent file",
			zap.String("path", torrentPath),
			zap.Error(err),
			zap.Int("data_size", len(torrentData)))
		return fmt.Errorf("failed to read torrent file: %v", err)
	}

	logger.Debug("Parsing torrent info",
		zap.String("torrent_path", torrentPath),
		zap.Int("data_size", len(torrentData)))
	info, err := bencode.Info(string(torrentData))
	if err != nil {
		logger.Error("Failed to parse torrent file",
			zap.Error(err),
			zap.String("torrent_path", torrentPath),
			zap.Int("data_size", len(torrentData)))
		return fmt.Errorf("failed to parse torrent file: %v", err)
	}
	logger.Info("Torrent info parsed",
		zap.String("announce", info.Announce),
		zap.Int("piece_length", info.Info.PieceLength),
		zap.Int("length", info.Info.Length),
		zap.Int("num_pieces", len(info.Info.Pieces)/20))

	peers, err := getPeers(info)
	if err != nil {
		logger.Error("Failed to get peers", zap.Error(err))
		return fmt.Errorf("failed to get peers: %v", err)
	}

	var lastErr error
	for _, peer := range peers {
		peerAddr := fmt.Sprintf("%s:%d", peer.IP, peer.Port)
		logger.Info("Attempting to connect to peer",
			zap.String("peer_addr", peerAddr))

		logger.Debug("Setting initial connection timeout",
			zap.String("peer_addr", peerAddr),
			zap.Duration("timeout", 1*time.Second))
		conn, err := net.DialTimeout("tcp", peerAddr, 1*time.Second)
		if err != nil {
			logger.Warn("Failed to connect to peer",
				zap.String("peer_addr", peerAddr),
				zap.Error(err))
			lastErr = err
			continue
		}
		defer conn.Close()

		logger.Debug("Setting handshake deadline",
			zap.String("peer_addr", peerAddr),
			zap.Time("deadline", time.Now().Add(1*time.Second)))
		if err := conn.SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
			logger.Warn("Failed to set deadline",
				zap.String("peer_addr", peerAddr),
				zap.Error(err))
			lastErr = err
			continue
		}

		_, infoHash, err := bencode.HashInfo(info)
		if err != nil {
			logger.Error("Failed to calculate info hash", zap.Error(err))
			return err
		}

		logger.Debug("Initiating handshake with peer",
			zap.String("peer_addr", peerAddr))
		response, err := performHandshake(conn, infoHash)
		if err != nil {
			logger.Warn("Handshake failed",
				zap.String("peer_addr", peerAddr),
				zap.Error(err))
			lastErr = err
			continue
		}
		logger.Debug("Handshake successful",
			zap.String("peer_addr", peerAddr),
			zap.Binary("peer_response", response))

		logger.Debug("Resetting connection deadlines",
			zap.String("peer_addr", peerAddr))
		if err := conn.SetDeadline(time.Time{}); err != nil {
			logger.Warn("Failed to reset deadlines",
				zap.String("peer_addr", peerAddr),
				zap.Error(err))
			lastErr = err
			continue
		}

		logger.Info("Starting piece download",
			zap.Int("index", pieceIndex),
			zap.Int("length", info.Info.PieceLength),
			zap.String("output", outputPath),
			zap.String("peer_addr", peerAddr))

		logger.Debug("Initiating message exchange for piece download",
			zap.String("peer_addr", peerAddr),
			zap.Int("piece_index", pieceIndex))
		if err := exchangeMessages(conn, pieceIndex, outputPath, info); err != nil {
			logger.Warn("Failed to download piece from peer",
				zap.String("peer_addr", peerAddr),
				zap.Error(err))
			lastErr = err
			continue
		}

		logger.Info("Piece download completed successfully",
			zap.Int("index", pieceIndex),
			zap.String("output", outputPath),
			zap.Int("piece_length", info.Info.PieceLength),
			zap.String("peer_addr", peerAddr))
		return nil
	}

	logger.Error("Failed to download piece from any peer",
		zap.Int("num_peers", len(peers)),
		zap.Error(lastErr))
	return fmt.Errorf("failed to download piece from any peer: %v", lastErr)
}

func getPeers(info *bencode.TorrentInfo) ([]peering.Peer, error) {
	_, infoHash, err := bencode.HashInfo(info)
	if err != nil {
		return nil, fmt.Errorf("failed to get info hash: %v", err)
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
		return nil, fmt.Errorf("failed to get tracker response: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	decoded, _, err := bencode.Decode[map[string]any](string(body))
	if err != nil {
		return nil, fmt.Errorf("failed to decode tracker response: %v", err)
	}

	peers := peering.ParsePeers(decoded["peers"].(string))
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers available")
	}

	return peers, nil
}

func performHandshake(conn net.Conn, infoHash []byte) ([]byte, error) {
	logger := zap.L().Named("perform_handshake").With(zap.String("peer_addr", conn.RemoteAddr().String()))

	handshake := make([]byte, 68)
	handshake[0] = 19
	copy(handshake[1:], []byte("BitTorrent protocol"))
	copy(handshake[28:], infoHash)
	copy(handshake[48:], []byte(peering.PeerID))

	if _, err := conn.Write(handshake); err != nil {
		logger.Error("Failed to send handshake", zap.Error(err))
		return nil, err
	}

	response := make([]byte, 68)
	if _, err := io.ReadFull(conn, response); err != nil {
		logger.Error("Failed to receive handshake response", zap.Error(err))
		return nil, fmt.Errorf("failed to receive handshake: %v", err)
	}

	if string(response[1:20]) != "BitTorrent protocol" {
		logger.Error("Invalid handshake response",
			zap.String("expected_protocol", "BitTorrent protocol"),
			zap.String("received_protocol", string(response[1:20])))
		return nil, fmt.Errorf("invalid handshake response")
	}

	return response, nil
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

	response, err := performHandshake(conn, infoHash)
	if err != nil {
		return err
	}

	responsePeerID := response[48:68]
	fmt.Printf("Peer ID: %x\n", responsePeerID)

	return nil
}

func exchangeMessages(conn net.Conn, pieceIndex int, outputPath string, info *bencode.TorrentInfo) error {
	actualLength := getPieceLength(pieceIndex, info)
	blocks := dividePiece(actualLength, 16384)

	msg, err := readMessage(conn)
	if err != nil {
		return fmt.Errorf("failed to read bitfield: %v", err)
	}
	if msg.ID != 5 {
		return fmt.Errorf("expected bitfield message, got %d", msg.ID)
	}

	if err := sendMessage(conn, 2, nil); err != nil {
		return fmt.Errorf("failed to send interested message: %v", err)
	}

	msg, err = readMessage(conn)
	if err != nil {
		return fmt.Errorf("failed to read unchoke message: %v", err)
	}
	if msg.ID != 1 {
		return fmt.Errorf("expected unchoke message, got %d", msg.ID)
	}

	pieceData := make([]byte, actualLength)

	for _, blk := range blocks {
		err := sendMessage(conn, 6, encodeRequest(pieceIndex, blk.Begin, blk.Length))
		if err != nil {
			return fmt.Errorf("failed to send request message: %v", err)
		}

		msg, err = readMessage(conn)
		if err != nil {
			return fmt.Errorf("failed to read piece message: %v", err)
		}
		if msg.ID != 7 {
			return fmt.Errorf("expected piece message, got %d", msg.ID)
		}

		if len(msg.Payload) < 8 {
			return fmt.Errorf("invalid piece message payload size")
		}
		receivedIndex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
		begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
		block := msg.Payload[8:]

		if receivedIndex != pieceIndex {
			return fmt.Errorf("received piece index %d does not match requested index %d",
				receivedIndex, pieceIndex)
		}

		copy(pieceData[begin:], block)
	}

	expectedHash := info.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
	actualHash := sha1.Sum(pieceData)
	if !bytes.Equal(actualHash[:], expectedHash) {
		return fmt.Errorf("piece hash mismatch")
	}

	if err := os.WriteFile(outputPath, pieceData, 0644); err != nil {
		return fmt.Errorf("failed to save piece: %v", err)
	}

	return nil
}

func readMessage(conn net.Conn) (*message, error) {
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read message length: %v", err)
	}

	if length == 0 {
		return &message{Length: length}, nil
	}

	msg := &message{Length: length}

	id := make([]byte, 1)
	if _, err := io.ReadFull(conn, id); err != nil {
		return nil, fmt.Errorf("failed to read message ID: %v", err)
	}
	msg.ID = id[0]

	payloadLen := int(length - 1)
	if payloadLen > 0 {
		msg.Payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(conn, msg.Payload); err != nil {
			return nil, fmt.Errorf("failed to read message payload: %v", err)
		}
	}

	return msg, nil
}

func sendMessage(conn net.Conn, id byte, payload []byte) error {
	var buf bytes.Buffer

	length := uint32(1 + len(payload))
	if err := binary.Write(&buf, binary.BigEndian, length); err != nil {
		return fmt.Errorf("failed to write message length: %v", err)
	}

	if err := buf.WriteByte(id); err != nil {
		return fmt.Errorf("failed to write message ID: %v", err)
	}

	if len(payload) > 0 {
		if _, err := buf.Write(payload); err != nil {
			return fmt.Errorf("failed to write message payload: %v", err)
		}
	}

	if _, err := conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("failed to send message: %v", err)
	}

	return nil
}

func dividePiece(pieceLength int, blockSize int) []block {
	var blocks []block
	for begin := 0; begin < pieceLength; begin += blockSize {
		end := begin + blockSize
		if end > pieceLength {
			end = pieceLength
		}
		blocks = append(blocks, block{Begin: begin, Length: end - begin})
	}
	return blocks
}

func encodeRequest(index, begin, length int) []byte {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))
	return payload
}

func getPieceLength(pieceIndex int, info *bencode.TorrentInfo) int {
	totalLength := info.Info.Length
	pieceLength := info.Info.PieceLength
	numPieces := (totalLength + pieceLength - 1) / pieceLength

	if pieceIndex == numPieces-1 {
		return totalLength - pieceLength*(numPieces-1)
	}
	return pieceLength
}
