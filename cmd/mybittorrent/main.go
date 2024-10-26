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

		decoded, _, err := decodeBencode(bencodedValue)
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

func decodeBencode(bencodedString string) (any, int, error) {
	if unicode.IsDigit(rune(bencodedString[0])) {
		return decodeBencodeString(bencodedString)
	} else if bencodedString[0] == 'i' {
		return decodeBencodeInteger(bencodedString)
	} else if bencodedString[0] == 'l' {
		return decodeBencodeList(bencodedString)
	} else {
		return "", 0, fmt.Errorf("Only strings, integers and lists are supported at the moment")
	}
}

func decodeBencodeList(bencodedString string) ([]any, int, error) {
	if bencodedString == "le" {
		return []any{}, 2, nil
	}

	if !strings.HasPrefix(bencodedString, "l") {
		return nil, 0, fmt.Errorf("invalid list format: must start with 'l'")
	}

	content := bencodedString[1:]
	result := make([]any, 0)
	totalLength := 1 // for the 'l'

	for len(content) > 0 {
		if content[0] == 'e' {
			return result, totalLength + 1, nil // +1 for the 'e'
		}

		value, consumed, err := decodeBencode(content)
		if err != nil {
			return nil, 0, err
		}

		content = content[consumed:]
		totalLength += consumed
		result = append(result, value)
	}

	return nil, 0, fmt.Errorf("invalid list format: missing end marker")
}

func decodeBencodeInteger(bencodedString string) (int, int, error) {
	if !strings.HasPrefix(bencodedString, "i") || !strings.HasSuffix(bencodedString, "e") {
		return 0, 0, fmt.Errorf("invalid integer format: must start with 'i' and end with 'e'")
	}

	endIndex := strings.Index(bencodedString, "e")
	if endIndex == -1 {
		return 0, 0, fmt.Errorf("invalid integer format: missing 'e' terminator")
	}

	numStr := bencodedString[1:endIndex]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid integer: %v", err)
	}

	return num, endIndex + 1, nil // 1 for the final 'e'
}

func decodeBencodeString(bencodedString string) (string, int, error) {
	firstColonIndex := strings.Index(bencodedString, ":")
	if firstColonIndex == -1 {
		return "", 0, fmt.Errorf("invalid string format: missing colon separator")
	}

	lengthStr := bencodedString[:firstColonIndex]
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", 0, err
	}

	totalLength := firstColonIndex + 1 + length // 1 for the ':' + length of number + string content
	return bencodedString[firstColonIndex+1 : firstColonIndex+1+length], totalLength, nil
}
