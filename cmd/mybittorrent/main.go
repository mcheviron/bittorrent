package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/codecrafters-io/bittorrent-starter-go/cmd/mybittorrent/bencode"
	"go.uber.org/zap"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

func init() {
	var err error
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(logger)
}

func main() {
	logger := zap.L()

	command := os.Args[1]

	if command == "decode" {
		bencodedValue := os.Args[2]

		decoded, _, err := bencode.Decode(bencodedValue)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))
	} else if command == "info" {
		if len(os.Args) < 3 {
			logger.Error("File path is required for info command")
			os.Exit(1)
		}
		filePath := os.Args[2]

		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			logger.Error("Failed to read file", zap.Error(err))
			os.Exit(1)
		}

		info, err := bencode.Info(string(fileContent))
		if err != nil {
			logger.Error("Failed to decode file content", zap.Error(err))
			os.Exit(1)
		}

		hash, err := bencode.HashInfo(info)
		if err != nil {
			logger.Error("Failed to encode info", zap.Error(err))
			os.Exit(1)
		}

		fmt.Printf("Tracker URL: %s\n", info.Announce)
		fmt.Printf("Length: %d\n", info.Info.Length)
		fmt.Printf("Info Hash: %s\n", hash)
		fmt.Printf("Piece Length: %d\n", info.Info.PieceLength)
		fmt.Println("Piece Hashes:")

		pieces := info.Info.Pieces
		for i := 0; i < len(pieces); i += 20 {
			pieceHash := pieces[i : i+20]
			fmt.Printf("%x\n", pieceHash)
		}
	} else {
		logger.Error("Unknown command", zap.String("command", command))
		os.Exit(1)
	}
}
