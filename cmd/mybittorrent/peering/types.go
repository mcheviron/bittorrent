package peering

import "net"

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

type Message struct {
	Length  uint32
	ID      byte
	Payload []byte
}

type Block struct {
	Begin  int
	Length int
}

const PeerID = "-MY0001-123456789012"
