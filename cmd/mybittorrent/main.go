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
	"sync"
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

	peers, err := getPeers(info)
	if err != nil {
		return fmt.Errorf("failed to get peers: %v", err)
	}

	var lastErr error
	for _, peer := range peers {
		peerAddr := fmt.Sprintf("%s:%d", peer.IP, peer.Port)

		conn, err := net.DialTimeout("tcp", peerAddr, 1*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		defer conn.Close()

		if err := conn.SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
			lastErr = err
			continue
		}

		_, infoHash, err := bencode.HashInfo(info)
		if err != nil {
			return err
		}

		_, err = performHandshake(conn, infoHash)
		if err != nil {
			lastErr = err
			continue
		}

		if err := conn.SetDeadline(time.Time{}); err != nil {
			lastErr = err
			continue
		}

		file, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create output file: %v", err)
		}
		defer file.Close()

		if err := exchangeMessages(conn, pieceIndex, file, info, true); err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	return fmt.Errorf("failed to download piece from any peer: %v", lastErr)
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

	peers, err := getPeers(info)
	if err != nil {
		return fmt.Errorf("failed to get peers: %v", err)
	}
	if len(peers) == 0 {
		return fmt.Errorf("no peers available")
	}

	totalPieces := len(info.Info.Pieces) / 20
	results := make(chan struct {
		index int
		data  []byte
		err   error
	}, totalPieces)

	type pieceWork struct {
		index int
		peer  peering.Peer
	}
	workChan := make(chan pieceWork, totalPieces)

	for i := range totalPieces {
		peerIndex := i % len(peers)
		workChan <- pieceWork{index: i, peer: peers[peerIndex]}
	}
	close(workChan)

	var workers sync.WaitGroup
	for range len(peers) {
		workers.Add(1)
		go func() {
			defer workers.Done()

			for work := range workChan {
				peerAddr := fmt.Sprintf("%s:%d", work.peer.IP, work.peer.Port)
				conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
				if err != nil {
					results <- struct {
						index int
						data  []byte
						err   error
					}{work.index, nil, fmt.Errorf("failed to connect to peer: %v", err)}
					continue
				}

				_, infoHash, err := bencode.HashInfo(info)
				if err != nil {
					conn.Close()
					results <- struct {
						index int
						data  []byte
						err   error
					}{work.index, nil, fmt.Errorf("failed to calculate info hash: %v", err)}
					continue
				}

				if _, err := performHandshake(conn, infoHash); err != nil {
					conn.Close()
					results <- struct {
						index int
						data  []byte
						err   error
					}{work.index, nil, fmt.Errorf("handshake failed: %v", err)}
					continue
				}

				var buffer bytes.Buffer
				err = exchangeMessages(conn, work.index, &buffer, info, true)
				conn.Close()

				results <- struct {
					index int
					data  []byte
					err   error
				}{work.index, buffer.Bytes(), err}
			}
		}()
	}

	totalLength := info.Info.Length
	fileData := make([]byte, totalLength)

	go func() {
		workers.Wait()
		close(results)
	}()

	completedPieces := 0
	for result := range results {
		if result.err != nil {
			return fmt.Errorf("failed to download piece %d: %v", result.index, result.err)
		}
		copy(fileData[result.index*info.Info.PieceLength:], result.data)
		completedPieces++
	}

	for pieceIndex := range totalPieces {
		pieceHash := info.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
		start := pieceIndex * info.Info.PieceLength
		end := start + info.Info.PieceLength
		if end > totalLength {
			end = totalLength
		}
		actualHash := sha1.Sum(fileData[start:end])
		if !bytes.Equal(actualHash[:], pieceHash) {
			return fmt.Errorf("hash mismatch for piece %d", pieceIndex)
		}
	}

	if err := os.WriteFile(outputPath, fileData, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %v", err)
	}

	return nil
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
	handshake := make([]byte, 68)
	handshake[0] = 19
	copy(handshake[1:], []byte("BitTorrent protocol"))
	copy(handshake[28:], infoHash)
	copy(handshake[48:], []byte(peering.PeerID))

	if _, err := conn.Write(handshake); err != nil {
		return nil, err
	}

	response := make([]byte, 68)
	if _, err := io.ReadFull(conn, response); err != nil {
		return nil, fmt.Errorf("failed to receive handshake: %v", err)
	}

	if string(response[1:20]) != "BitTorrent protocol" {
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

func exchangeMessages(conn net.Conn, pieceIndex int, output io.Writer, info *bencode.TorrentInfo, handleInitialMessages bool) error {
	actualLength := getPieceLength(pieceIndex, info)
	blocks := dividePiece(actualLength, 16384)

	if handleInitialMessages {
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
	}

	pieceData := make([]byte, actualLength)

	for _, blk := range blocks {
		err := sendMessage(conn, 6, encodeRequest(pieceIndex, blk.Begin, blk.Length))
		if err != nil {
			return fmt.Errorf("failed to send request message: %v", err)
		}

		msg, err := readMessage(conn)
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

	if _, err := output.Write(pieceData); err != nil {
		return fmt.Errorf("failed to write piece data: %v", err)
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
