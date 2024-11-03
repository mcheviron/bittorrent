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
)

// Client represents a BitTorrent client that manages peer connections and downloads.
type Client struct {
	info     *bencode.TorrentInfo
	peers    []Peer
	infoHash []byte
}

// NewClient creates a new BitTorrent client with the given torrent info.
// It initializes the client by fetching peers and calculating the info hash.
// Returns an error if peer discovery or info hash calculation fails.
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

// GetPeers fetches a list of peers from the tracker for the given torrent info.
// It sends a tracker request with the required parameters and parses the response.
// Returns a list of peers or an error if the tracker request fails.
func GetPeers(info *bencode.TorrentInfo) ([]Peer, error) {
	_, infoHash, err := bencode.HashInfo(info)
	if err != nil {
		return nil, fmt.Errorf("failed to get info hash: %v", err)
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

	peersData, ok := decoded["peers"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid peers data type in tracker response: got %T, want string", decoded["peers"])
	}

	peers, err := ParsePeers(peersData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse peers: %v", err)
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers available")
	}

	return peers, nil
}

// DownloadPiece downloads a specific piece from available peers.
// It attempts to download from each peer until successful, using a round-robin approach.
// If a peer fails, it continues with the next peer until either success or all peers fail.
// Returns the piece data or an error if all download attempts fail.
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

// DownloadAll downloads all pieces of the torrent file concurrently.
// It implements a worker pool pattern where:
//   - Work is distributed among peers in a round-robin fashion
//   - Multiple workers download pieces concurrently
//   - Results are assembled in order and verified against piece hashes
//
// Returns the complete file data or an error if the download fails.
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
	peerAddr := fmt.Sprintf("%s:%d", peer.IP, peer.Port)

	conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to peer: %v", err)
	}
	defer conn.Close()

	if _, err := PerformHandshake(conn, c.infoHash); err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	if err := c.exchangeMessages(conn, pieceIndex, &buffer); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func (c *Client) distributePieceWork(totalPieces int) <-chan pieceWork {
	workChan := make(chan pieceWork, totalPieces)
	for i := range totalPieces {
		peerIndex := i % len(c.peers) // round-robin
		workChan <- pieceWork{index: i, peer: c.peers[peerIndex]}
	}
	close(workChan)
	return workChan
}

func (c *Client) startWorkers(workChan <-chan pieceWork, results chan<- pieceResult) {
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

func (c *Client) assembleFile(results <-chan pieceResult, totalPieces int) ([]byte, error) {
	totalLength := c.info.Info.Length
	fileData := make([]byte, totalLength)

	for result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("failed to download piece %d: %v", result.index, result.err)
		}
		copy(fileData[result.index*c.info.Info.PieceLength:], result.data)
	}

	// verify pieces
	for pieceIndex := range totalPieces {
		pieceHash := c.info.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
		start := pieceIndex * c.info.Info.PieceLength
		end := min(start+c.info.Info.PieceLength, totalLength)
		actualHash := sha1.Sum(fileData[start:end])
		if !bytes.Equal(actualHash[:], pieceHash) {
			return nil, fmt.Errorf("hash mismatch for piece %d", pieceIndex)
		}
	}

	return fileData, nil
}

func (c *Client) exchangeMessages(conn net.Conn, pieceIndex int, output io.Writer) error {
	actualLength := c.getPieceLength(pieceIndex)
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
			return fmt.Errorf("received piece index %d does not match requested index %d", receivedIndex, pieceIndex)
		}

		copy(pieceData[begin:], block)
	}

	expectedHash := c.info.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
	actualHash := sha1.Sum(pieceData)
	if !bytes.Equal(actualHash[:], expectedHash) {
		return fmt.Errorf("piece hash mismatch")
	}

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
