package peering

import (
	"encoding/binary"
)

// dividePiece splits a piece into blocks of specified size
// Using range and pre-allocation for better performance
func dividePiece(pieceLength int, blockSize int) []Block {
	numBlocks := (pieceLength + blockSize - 1) / blockSize
	blocks := make([]Block, 0, numBlocks)

	for begin := 0; begin < pieceLength; begin += blockSize {
		end := min(begin+blockSize, pieceLength)
		blocks = append(blocks, Block{
			Begin:  begin,
			Length: end - begin,
		})
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
