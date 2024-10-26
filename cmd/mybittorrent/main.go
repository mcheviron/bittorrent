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

// decodeBencode decodes a bencoded string into its corresponding value.
// The function currently only supports decoding strings encoded in the bencode format.
//
// Bencode string format: <length>:<contents>
// Where:
//   - length: An integer specifying the length of the contents
//   - contents: The actual string content
//
// Examples:
//   - Input: "5:hello" -> Output: "hello"
//   - Input: "10:hello12345" -> Output: "hello12345"
//
// Returns:
//   - any: The decoded value (currently only strings)
//   - error: An error if the decoding fails or if a non-string type is encountered
//
// Note: This implementation is limited to strings only and could be expanded
// to support other bencode types like integers, lists, and dictionaries.
func decodeBencode(bencodedString string) (any, error) {
	if unicode.IsDigit(rune(bencodedString[0])) {
		firstColonIndex := strings.Index(bencodedString, ":")

		lengthStr := bencodedString[:firstColonIndex]

		length, err := strconv.Atoi(lengthStr)
		if err != nil {
			return "", err
		}

		return bencodedString[firstColonIndex+1 : firstColonIndex+1+length], nil
	} else {
		return "", fmt.Errorf("Only strings are supported at the moment")
	}
}
func main() {
	logger := zap.L()
	logger.Info("Logs from your program will appear here!")

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
