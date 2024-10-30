package peering

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/bencode"
	"go.uber.org/zap"
)

type Client struct {
	info     *bencode.TorrentInfo
	peers    []Peer
	infoHash []byte
}

func NewClient(info *bencode.TorrentInfo) (*Client, error) {
	peers, err := GetPeers(info)
	if err != nil {
		return nil, err
	}

	_, infoHash, err := bencode.HashInfo(info)
	if err != nil {
		return nil, err
	}

	return &Client{
		info:     info,
		peers:    peers,
		infoHash: infoHash,
	}, nil
}

func GetPeers(info *bencode.TorrentInfo) ([]Peer, error) {
	_, infoHash, err := bencode.HashInfo(info)
	if err != nil {
		return nil, fmt.Errorf("failed to get info hash: %v", err)
	}

	trackerReq := &TrackerRequest{
		InfoHash:   infoHash,
		PeerID:     PeerID,
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

	peers := ParsePeers(decoded["peers"].(string))
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers available")
	}

	return peers, nil
}

func (c *Client) DownloadPiece(pieceIndex int) ([]byte, error) {
	var lastErr error
	for _, peer := range c.peers {
		data, err := c.downloadPieceFromPeer(peer, pieceIndex)
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("failed to download piece from any peer: %v", lastErr)
}

func (c *Client) DownloadAll() ([]byte, error) {
	totalPieces := len(c.info.Info.Pieces) / 20
	results := make(chan pieceResult, totalPieces)

	workChan := c.distributePieceWork(totalPieces)
	c.startWorkers(workChan, results)

	return c.assembleFile(results, totalPieces)
}

type pieceWork struct {
	index int
	peer  Peer
}

type pieceResult struct {
	index int
	data  []byte
	err   error
}

func (c *Client) downloadPieceFromPeer(peer Peer, pieceIndex int) ([]byte, error) {
	logger := zap.L().Named("downloadPieceFromPeer")
	peerAddr := fmt.Sprintf("%s:%d", peer.IP, peer.Port)
	logger.Info("Connecting to peer", zap.String("peerAddr", peerAddr))

	conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
	if err != nil {
		logger.Error("Failed to connect to peer", zap.String("peerAddr", peerAddr), zap.Error(err))
		return nil, fmt.Errorf("failed to connect to peer: %v", err)
	}
	defer conn.Close()

	logger.Info("Performing handshake", zap.String("peerAddr", peerAddr))
	if _, err := PerformHandshake(conn, c.infoHash); err != nil {
		logger.Error("Handshake failed", zap.String("peerAddr", peerAddr), zap.Error(err))
		return nil, err
	}

	var buffer bytes.Buffer
	logger.Info("Exchanging messages", zap.String("peerAddr", peerAddr), zap.Int("pieceIndex", pieceIndex))
	if err := c.exchangeMessages(conn, pieceIndex, &buffer); err != nil {
		logger.Error("Failed to exchange messages", zap.String("peerAddr", peerAddr), zap.Int("pieceIndex", pieceIndex), zap.Error(err))
		return nil, err
	}

	logger.Info("Successfully downloaded piece", zap.String("peerAddr", peerAddr), zap.Int("pieceIndex", pieceIndex))
	return buffer.Bytes(), nil
}

func (c *Client) distributePieceWork(totalPieces int) chan pieceWork {
	workChan := make(chan pieceWork, totalPieces)
	for i := range totalPieces {
		peerIndex := i % len(c.peers)
		workChan <- pieceWork{index: i, peer: c.peers[peerIndex]}
	}
	close(workChan)
	return workChan
}

func (c *Client) startWorkers(workChan chan pieceWork, results chan pieceResult) {
	var workers sync.WaitGroup
	for range len(c.peers) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for work := range workChan {
				data, err := c.downloadPieceFromPeer(work.peer, work.index)
				results <- pieceResult{
					index: work.index,
					data:  data,
					err:   err,
				}
			}
		}()
	}

	go func() {
		workers.Wait()
		close(results)
	}()
}

func (c *Client) assembleFile(results chan pieceResult, totalPieces int) ([]byte, error) {
	totalLength := c.info.Info.Length
	fileData := make([]byte, totalLength)

	for result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("failed to download piece %d: %v", result.index, result.err)
		}
		copy(fileData[result.index*c.info.Info.PieceLength:], result.data)
	}

	// Verify all pieces
	for pieceIndex := range totalPieces {
		pieceHash := c.info.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
		start := pieceIndex * c.info.Info.PieceLength
		end := start + c.info.Info.PieceLength
		if end > totalLength {
			end = totalLength
		}
		actualHash := sha1.Sum(fileData[start:end])
		if !bytes.Equal(actualHash[:], pieceHash) {
			return nil, fmt.Errorf("hash mismatch for piece %d", pieceIndex)
		}
	}

	return fileData, nil
}

func (c *Client) exchangeMessages(conn net.Conn, pieceIndex int, output io.Writer) error {
	logger := zap.L().Named("exchangeMessages")
	actualLength := c.getPieceLength(pieceIndex)
	blocks := dividePiece(actualLength, 16384)

	// Handle bitfield
	logger.Info("Reading bitfield message")
	msg, err := readMessage(conn)
	if err != nil {
		logger.Error("Failed to read bitfield", zap.Error(err))
		return fmt.Errorf("failed to read bitfield: %v", err)
	}
	if msg.ID != 5 {
		logger.Error("Expected bitfield message", zap.Int("msgID", int(msg.ID)))
		return fmt.Errorf("expected bitfield message, got %d", msg.ID)
	}

	// Send interested
	logger.Info("Sending interested message")
	if err := sendMessage(conn, 2, nil); err != nil {
		logger.Error("Failed to send interested message", zap.Error(err))
		return fmt.Errorf("failed to send interested message: %v", err)
	}

	// Receive unchoke
	logger.Info("Reading unchoke message")
	msg, err = readMessage(conn)
	if err != nil {
		logger.Error("Failed to read unchoke message", zap.Error(err))
		return fmt.Errorf("failed to read unchoke message: %v", err)
	}
	if msg.ID != 1 {
		logger.Error("Expected unchoke message", zap.Int("msgID", int(msg.ID)))
		return fmt.Errorf("expected unchoke message, got %d", msg.ID)
	}

	pieceData := make([]byte, actualLength)

	// Download all blocks
	for _, blk := range blocks {
		logger.Info("Sending request message", zap.Int("pieceIndex", pieceIndex), zap.Int("begin", blk.Begin), zap.Int("length", blk.Length))
		err := sendMessage(conn, 6, encodeRequest(pieceIndex, blk.Begin, blk.Length))
		if err != nil {
			logger.Error("Failed to send request message", zap.Error(err))
			return fmt.Errorf("failed to send request message: %v", err)
		}

		logger.Info("Reading piece message")
		msg, err := readMessage(conn)
		if err != nil {
			logger.Error("Failed to read piece message", zap.Error(err))
			return fmt.Errorf("failed to read piece message: %v", err)
		}
		if msg.ID != 7 {
			logger.Error("Expected piece message", zap.Int("msgID", int(msg.ID)))
			return fmt.Errorf("expected piece message, got %d", msg.ID)
		}

		if len(msg.Payload) < 8 {
			logger.Error("Invalid piece message payload size", zap.Int("payloadSize", len(msg.Payload)))
			return fmt.Errorf("invalid piece message payload size")
		}
		receivedIndex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
		begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
		block := msg.Payload[8:]

		if receivedIndex != pieceIndex {
			logger.Error("Received piece index does not match requested index", zap.Int("receivedIndex", receivedIndex), zap.Int("requestedIndex", pieceIndex))
			return fmt.Errorf("received piece index %d does not match requested index %d", receivedIndex, pieceIndex)
		}

		copy(pieceData[begin:], block)
	}

	// Verify piece hash
	logger.Info("Verifying piece hash", zap.Int("pieceIndex", pieceIndex))
	expectedHash := c.info.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
	actualHash := sha1.Sum(pieceData)
	if !bytes.Equal(actualHash[:], expectedHash) {
		logger.Error("Piece hash mismatch", zap.Int("pieceIndex", pieceIndex))
		return fmt.Errorf("piece hash mismatch")
	}

	logger.Info("Writing piece data to output", zap.Int("pieceIndex", pieceIndex))
	_, err = output.Write(pieceData)
	return err
}

func (c *Client) getPieceLength(pieceIndex int) int {
	totalLength := c.info.Info.Length
	pieceLength := c.info.Info.PieceLength
	numPieces := (totalLength + pieceLength - 1) / pieceLength

	if pieceIndex == numPieces-1 {
		return totalLength - pieceLength*(numPieces-1)
	}
	return pieceLength
}
