package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

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

		decoded, err := decodeBencode(bencodedValue)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))
	} else {
		logger.Error("Unknown command", zap.String("command", command))
		os.Exit(1)
	}
}

func decodeBencode(bencodedString string) (any, error) {
	if unicode.IsDigit(rune(bencodedString[0])) {
		return decodeBencodeString(bencodedString)
	} else if bencodedString[0] == 'i' {
		return decodeBencodeInteger(bencodedString)
	} else {
		return "", fmt.Errorf("Only strings and integers are supported at the moment")
	}
}

func decodeBencodeInteger(bencodedString string) (int, error) {
	if !strings.HasPrefix(bencodedString, "i") || !strings.HasSuffix(bencodedString, "e") {
		return 0, fmt.Errorf("invalid integer format: must start with 'i' and end with 'e'")
	}

	numStr := bencodedString[1 : len(bencodedString)-1]

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %v", err)
	}

	return num, nil
}

func decodeBencodeString(bencodedString string) (string, error) {
	firstColonIndex := strings.Index(bencodedString, ":")

	lengthStr := bencodedString[:firstColonIndex]

	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", err
	}

	return bencodedString[firstColonIndex+1 : firstColonIndex+1+length], nil
}
