package utils

import (
	"bytes"
	"compress/gzip"
)

// Gzip compresses a byte slice using standard GZIP compression.
// Used across the project for county boundaries, vector tiles, and cached responses.
func Gzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
