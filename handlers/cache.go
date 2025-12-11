package handlers

import (
	"bytes"
	"compress/gzip"
	"sync"
)

// SimpleCache is a thread-safe in-memory cache for gzipped responses.
// Entries persist indefinitely (until server restart) since county boundaries are static.
type SimpleCache struct {
	mu    sync.RWMutex
	items map[string][]byte
}

// NewSimpleCache creates and returns a new cache instance.
func NewSimpleCache() *SimpleCache {
	return &SimpleCache{
		items: make(map[string][]byte),
	}
}

// Set stores gzipped data in the cache under the given key.
func (c *SimpleCache) Set(key string, gzipData []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = gzipData
}

// Get retrieves gzipped data from the cache. Returns (data, true) if found, (nil, false) otherwise.
func (c *SimpleCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.items[key]
	return data, ok
}

// Clear removes all entries from the cache.
func (c *SimpleCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string][]byte)
}

// GzipBytes compresses data using gzip and returns the compressed bytes.
// Used to reduce response payload size for large GeoJSON responses.
func GzipBytes(data []byte) ([]byte, error) {
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

// countyCache is the package-level cache instance for county boundary responses.
var countyCache = NewSimpleCache()
