package magnet

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// Link represents a parsed magnet link with its components
type Link struct {
	InfoHash   string
	Name       string
	Trackers   []string
	ExactTopic string
}

// Parse parses a magnet URI and returns a Link object containing the extracted information.
// It validates and extracts the info hash, display name and tracker URLs.
// Returns an error if the URI format is invalid or required fields are missing/malformed.
func Parse(uri string) (*Link, error) {
	if !strings.HasPrefix(uri, "magnet:?") {
		return nil, fmt.Errorf("invalid magnet URI format")
	}

	queryStr := uri[8:]
	values, err := url.ParseQuery(queryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse magnet URI query: %w", err)
	}

	xt := values.Get("xt")
	if !strings.HasPrefix(xt, "urn:btih:") {
		return nil, fmt.Errorf("invalid or missing urn:btih prefix in xt parameter")
	}

	infoHash := strings.TrimPrefix(xt, "urn:btih:")
	if len(infoHash) != 40 {
		return nil, fmt.Errorf("invalid info hash length")
	}
	if _, err := hex.DecodeString(infoHash); err != nil {
		return nil, fmt.Errorf("invalid hex-encoded info hash: %w", err)
	}

	return &Link{
		ExactTopic: xt,
		InfoHash:   infoHash,
		Name:       values.Get("dn"),
		Trackers:   values["tr"],
	}, nil
}
