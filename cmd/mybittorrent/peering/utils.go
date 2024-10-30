package peering

import (
	"encoding/binary"

	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/bencode"
)

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

func getPieceLength(pieceIndex int, info *bencode.TorrentInfo) int {
	totalLength := info.Info.Length
	pieceLength := info.Info.PieceLength
	numPieces := (totalLength + pieceLength - 1) / pieceLength

	if pieceIndex == numPieces-1 {
		return totalLength - pieceLength*(numPieces-1)
	}
	return pieceLength
}
