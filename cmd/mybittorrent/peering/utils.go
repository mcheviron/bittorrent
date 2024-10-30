package peering

import "encoding/binary"

func dividePiece(pieceLength int, blockSize int) []Block {
	var blocks []Block
	for begin := 0; begin < pieceLength; begin += blockSize {
		end := begin + blockSize
		if end > pieceLength {
			end = pieceLength
		}
		blocks = append(blocks, Block{Begin: begin, Length: end - begin})
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

// Other utility functions...
