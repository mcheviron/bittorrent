package peering

import (
	"encoding/binary"
	"net"
)

// ParsePeers converts a compact peer string into a slice of Peer structs
func ParsePeers(peersData string) []Peer {
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
