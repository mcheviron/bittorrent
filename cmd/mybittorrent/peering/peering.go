package peering

import (
	"encoding/binary"
	"net"
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

const PeerID = "-MY0001-123456789012"

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
