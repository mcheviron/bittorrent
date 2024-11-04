package peering

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// Reserved bytes with extension bit (20th bit) set
var reservedBytes = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00}

// PerformHandshake performs the BitTorrent handshake with a peer
// Changed from performHandshake to PerformHandshake to make it public
func PerformHandshake(conn net.Conn, infoHash []byte) ([]byte, error) {
	// Construct handshake message
	pstr := "BitTorrent protocol"
	handshake := make([]byte, 0, 68)
	handshake = append(handshake, byte(len(pstr)))
	handshake = append(handshake, pstr...)
	handshake = append(handshake, reservedBytes...) // Use the new reserved bytes
	handshake = append(handshake, infoHash...)
	handshake = append(handshake, []byte(peerID)...)

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

func readMessage(conn net.Conn) (*Message, error) {
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read message length: %v", err)
	}

	if length == 0 {
		return &Message{Length: length}, nil
	}

	msg := &Message{Length: length}

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
