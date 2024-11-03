package peering

import (
	"encoding/binary"
	"fmt"
	"net"
)

// ParsePeers takes a string containing concatenated peer information in the BitTorrent format
// and parses it into a slice of Peer structs. Each peer in the input string is represented
// by 6 consecutive bytes - 4 bytes for IP address followed by 2 bytes for port number.
//
// The input string format follows the BitTorrent specification where:
//   - First 4 bytes represent IPv4 address
//   - Next 2 bytes represent port number in network byte order (big-endian)
//
// Parameters:
//   - peersData: A string containing the raw concatenated peer data
//
// Returns:
//   - []Peer: A slice of Peer structs, each containing an IP address and port number
func ParsePeers(peersData string) ([]Peer, error) {
	if len(peersData)%6 != 0 {
		return nil, fmt.Errorf("invalid peers data length: %d (must be multiple of 6)", len(peersData))
	}

	peers := make([]Peer, 0, len(peersData)/6)

	for i := 0; i < len(peersData); i += 6 {
		peer := Peer{
			IP:   net.IP([]byte(peersData[i : i+4])),
			Port: binary.BigEndian.Uint16([]byte(peersData[i+4 : i+6])),
		}
		peers = append(peers, peer)
	}

	return peers, nil
}
