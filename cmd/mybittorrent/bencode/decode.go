package bencode

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type TorrentInfo struct {
	Announce  string    `json:"announce"`
	CreatedBy string    `json:"created by"`
	Info      InnerInfo `json:"info"`
}

type InnerInfo struct {
	Length      int    `json:"length"`
	Name        string `json:"name"`
	PieceLength int    `json:"piece length"`
	Pieces      []byte `json:"pieces"`
}

func Info(bencodedString string) (*TorrentInfo, error) {
	decoded, _, err := Decode[any](bencodedString)
	if err != nil {
		return nil, err
	}

	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("decoded data is not a dictionary")
	}

	torrentInfo := &TorrentInfo{}

	if announce, ok := decodedMap["announce"].(string); ok {
		torrentInfo.Announce = announce
	}

	if createdBy, ok := decodedMap["created by"].(string); ok {
		torrentInfo.CreatedBy = createdBy
	}

	if info, ok := decodedMap["info"].(map[string]any); ok {
		if length, ok := info["length"].(int); ok {
			torrentInfo.Info.Length = length
		}

		if name, ok := info["name"].(string); ok {
			torrentInfo.Info.Name = name
		}

		if pieceLength, ok := info["piece length"].(int); ok {
			torrentInfo.Info.PieceLength = pieceLength
		}

		if pieces, ok := info["pieces"].(string); ok {
			torrentInfo.Info.Pieces = []byte(pieces)
		}
	}

	return torrentInfo, nil
}

func Decode[T any](bencodedString string) (T, int, error) {
	var result T
	if unicode.IsDigit(rune(bencodedString[0])) {
		str, length, err := decodeString(bencodedString)
		if err != nil {
			return result, 0, err
		}
		result = any(str).(T)
		return result, length, nil
	} else if bencodedString[0] == 'i' {
		intVal, length, err := decodeInteger(bencodedString)
		if err != nil {
			return result, 0, err
		}
		result = any(intVal).(T)
		return result, length, nil
	} else if bencodedString[0] == 'l' {
		list, length, err := decodeList(bencodedString)
		if err != nil {
			return result, 0, err
		}
		result = any(list).(T)
		return result, length, nil
	} else if bencodedString[0] == 'd' {
		dict, length, err := decodeDictionary(bencodedString)
		if err != nil {
			return result, 0, err
		}
		result = any(dict).(T)
		return result, length, nil
	} else {
		return result, 0, fmt.Errorf("unsupported bencoded type: %s", string(bencodedString[0]))
	}
}

func decodeDictionary(bencodedString string) (map[string]any, int, error) {
	if bencodedString == "de" {
		return map[string]any{}, 2, nil
	}

	if !strings.HasPrefix(bencodedString, "d") || !strings.HasSuffix(bencodedString, "e") {
		return nil, 0, fmt.Errorf("invalid dictionary format: must start with 'd' and end with 'e'")
	}

	content := bencodedString[1:]
	result := make(map[string]any)
	totalLength := 1 // for the 'd'

	for len(content) > 0 {
		if content[0] == 'e' {
			return result, totalLength + 1, nil // +1 for the 'e'
		}

		key, keyLength, err := decodeString(content)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid dictionary key: %v", err)
		}

		content = content[keyLength:]
		totalLength += keyLength

		value, valueLength, err := Decode[any](content)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid dictionary value: %v", err)
		}

		content = content[valueLength:]
		totalLength += valueLength
		result[key] = value
	}

	return nil, 0, fmt.Errorf("invalid dictionary format: missing end marker")
}

func decodeList(bencodedString string) ([]any, int, error) {
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

		value, consumed, err := Decode[any](content)
		if err != nil {
			return nil, 0, err
		}

		content = content[consumed:]
		totalLength += consumed
		result = append(result, value)
	}

	return nil, 0, fmt.Errorf("invalid list format: missing end marker")
}

func decodeInteger(bencodedString string) (int, int, error) {
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

func decodeString(bencodedString string) (string, int, error) {
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
