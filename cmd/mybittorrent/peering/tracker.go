package peering

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/bencode"
)

func GetPeersFromTracker(trackerURL string, infoHash []byte) ([]Peer, error) {
	params := url.Values{
		"info_hash":  []string{string(infoHash)},
		"peer_id":    []string{peerID},
		"port":       []string{"6881"},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"left":       []string{"100"},
		"compact":    []string{"1"},
	}

	fullURL := fmt.Sprintf("%s?%s", trackerURL, params.Encode())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to contact tracker: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tracker response: %w", err)
	}

	trackerResp, _, err := bencode.Decode[map[string]any](string(body))
	if err != nil {
		return nil, fmt.Errorf("failed to decode tracker response: %w", err)
	}

	peersData, ok := trackerResp["peers"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid peers data format")
	}

	var peers []Peer
	peerCount := len(peersData) / 6

	for i := 0; i < peerCount; i++ {
		offset := i * 6
		ip := net.IPv4(
			peersData[offset],
			peersData[offset+1],
			peersData[offset+2],
			peersData[offset+3],
		)
		port := binary.BigEndian.Uint16([]byte(peersData[offset+4 : offset+6]))
		peers = append(peers, Peer{
			IP:   ip,
			Port: port,
		})
	}

	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers found")
	}

	return peers, nil
}
