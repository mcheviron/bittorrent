package bencode

import (
	"crypto/sha1"
	"fmt"
	"slices"
)

func Encode(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return fmt.Sprintf("%d:%s", len(v), v), nil
	case []byte: // for info.pieces
		return fmt.Sprintf("%d:%s", len(v), string(v)), nil
	case int:
		return fmt.Sprintf("i%de", v), nil
	case []any:
		result := "l"
		for _, item := range v {
			encoded, err := Encode(item)
			if err != nil {
				return "", fmt.Errorf("failed to encode list item: %v", err)
			}
			result += encoded
		}
		return result + "e", nil
	case map[string]any:
		result := "d"
		// Sort keys for consistent encoding
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		for _, key := range keys {
			keyEncoded, err := Encode(key)
			if err != nil {
				return "", fmt.Errorf("failed to encode dictionary key: %v", err)
			}
			valueEncoded, err := Encode(v[key])
			if err != nil {
				return "", fmt.Errorf("failed to encode dictionary value: %v", err)
			}
			result += keyEncoded + valueEncoded
		}
		return result + "e", nil
	default:
		return "", fmt.Errorf("unsupported type for bencode encoding: %T", value)
	}
}
func HashInfo(info *TorrentInfo) (string, []byte, error) {
	infoMap := map[string]any{
		"length":       info.Info.Length,
		"name":         info.Info.Name,
		"piece length": info.Info.PieceLength,
		"pieces":       info.Info.Pieces,
	}

	encoded, err := Encode(infoMap)
	if err != nil {
		return "", nil, fmt.Errorf("failed to encode info: %v", err)
	}

	h := sha1.New()
	h.Write([]byte(encoded))
	infoHash := h.Sum(nil)
	return fmt.Sprintf("%x", infoHash), infoHash, nil
}
