package tiles

import (
	"container/list"
	"fmt"
	"sync"
	atomic "sync/atomic"

	lru "github.com/hashicorp/golang-lru/v2"
)

// TileKey represents a unique identifier for a tile
type TileKey struct {
	Z, X, Y int
}

// TileEntry stores the tile data and its size
type TileEntry struct {
	key  TileKey
	data []byte
	size int64
}

// StringEntry stores string-keyed tile data and size.
type StringEntry struct {
	key  string
	data []byte
	size int64
}

// ZoomLRU is a least-recently-used cache for a specific zoom level with a byte quota
type ZoomLRU struct {
	maxBytes  int64
	currBytes int64
	ll        *list.List
	cache     map[TileKey]*list.Element
	mu        sync.RWMutex
}

// Get retrieves a tile from the cache
func (c *ZoomLRU) Get(key TileKey) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ele, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		return ele.Value.(*TileEntry).data, true
	}
	return nil, false
}

// Put adds a tile to the cache, evicting old tiles if the quota is exceeded
func (c *ZoomLRU) Put(key TileKey, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entrySize := int64(len(data))

	// If entry itself is larger than maxBytes, don't cache it
	if entrySize > c.maxBytes {
		return
	}

	if ele, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		oldEntry := ele.Value.(*TileEntry)
		c.currBytes -= oldEntry.size
		oldEntry.data = data
		oldEntry.size = entrySize
		c.currBytes += entrySize
	} else {
		ele := c.ll.PushFront(&TileEntry{key: key, data: data, size: entrySize})
		c.cache[key] = ele
		c.currBytes += entrySize
	}

	// Remove items until under maxBytes
	for c.currBytes > c.maxBytes {
		c.RemoveOldest()
	}
}

func (c *ZoomLRU) RemoveOldest() {
	ele := c.ll.Back()
	if ele != nil {
		c.ll.Remove(ele)
		kv := ele.Value.(*TileEntry)
		delete(c.cache, kv.key)
		c.currBytes -= kv.size
	}
}

// TileCache manages multiple zoom-specific LRU caches
type TileCache struct {
	zooms map[int]*ZoomLRU
}

// StringLRU is a byte-bounded LRU cache keyed by string.
type StringLRU struct {
	maxBytes  int64
	currBytes int64
	ll        *list.List
	cache     map[string]*list.Element
	mu        sync.RWMutex
}

// Get retrieves a tile from the appropriate zoom cache
func (tc *TileCache) Get(z, x, y int) ([]byte, bool) {
	if cache, ok := tc.zooms[z]; ok {
		return cache.Get(TileKey{z, x, y})
	}
	return nil, false
}

// Put adds a tile to the appropriate zoom cache
func (tc *TileCache) Put(z, x, y int, data []byte) {
	if cache, ok := tc.zooms[z]; ok {
		cache.Put(TileKey{z, x, y}, data)
	}
}

// Stats returns the current memory usage of the cache for debugging
func (tc *TileCache) Stats() string {
	stats := "Tile Cache Stats:\n"
	var totalBytes int64
	for z := 13; z <= 19; z++ {
		if c, ok := tc.zooms[z]; ok {
			c.mu.RLock()
			stats += fmt.Sprintf("  Zoom %d: %d tiles, %.2f MB / %.2f MB\n",
				z, len(c.cache), float64(c.currBytes)/1024/1024, float64(c.maxBytes)/1024/1024)
			totalBytes += c.currBytes
			c.mu.RUnlock()
		}
	}
	stats += fmt.Sprintf("Total Usage: %.2f MB\n", float64(totalBytes)/1024/1024)
	return stats
}

// Tiles cache instances
var ParcelTilesCache *TileCache
var CountyTilesCache *sync.Map // Simple map for county tiles (no eviction, few tiles)
var PMTilesCache *lru.Cache[string, []byte]
var PMTilesSize uint64
var TaxHeatmapTilesCache *StringLRU
var TaxParcelTilesCache *StringLRU

// NewZoomLRU creates a new LRU cache for a specific zoom level
func NewZoomLRU(maxBytes int64) *ZoomLRU {
	return &ZoomLRU{
		maxBytes: maxBytes,
		ll:       list.New(),
		cache:    make(map[TileKey]*list.Element),
	}
}

// NewStringLRU creates a new byte-bounded LRU cache keyed by string.
func NewStringLRU(maxBytes int64) *StringLRU {
	return &StringLRU{
		maxBytes: maxBytes,
		ll:       list.New(),
		cache:    make(map[string]*list.Element),
	}
}

// Get retrieves cached bytes by string key.
func (c *StringLRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ele, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		return ele.Value.(*StringEntry).data, true
	}
	return nil, false
}

// Put stores bytes by string key with LRU eviction by total bytes.
func (c *StringLRU) Put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entrySize := int64(len(data))
	if entrySize > c.maxBytes {
		return
	}

	if ele, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		oldEntry := ele.Value.(*StringEntry)
		c.currBytes -= oldEntry.size
		oldEntry.data = data
		oldEntry.size = entrySize
		c.currBytes += entrySize
	} else {
		ele := c.ll.PushFront(&StringEntry{key: key, data: data, size: entrySize})
		c.cache[key] = ele
		c.currBytes += entrySize
	}

	for c.currBytes > c.maxBytes {
		c.removeOldest()
	}
}

func (c *StringLRU) removeOldest() {
	ele := c.ll.Back()
	if ele != nil {
		c.ll.Remove(ele)
		kv := ele.Value.(*StringEntry)
		delete(c.cache, kv.key)
		c.currBytes -= kv.size
	}
}

// Stats returns entry count and byte usage for this cache.
func (c *StringLRU) Stats(name string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("%s:\n  Entries: %d\n  Cache Size: %.2f MB / %.2f MB\n",
		name, len(c.cache), float64(c.currBytes)/1024/1024, float64(c.maxBytes)/1024/1024)
}

// InitPMTilesCache initializes the LRU cache for PMTiles range requests
func InitPMTilesCache() error {
	var err error
	// Use NewWithEvict to track total byte size
	PMTilesCache, err = lru.NewWithEvict(3000, func(key string, value []byte) {
		atomic.AddUint64(&PMTilesSize, ^uint64(len(value)-1))
	})
	return err
}

// InitParcelTilesCache initializes the global cache with predefined quotas per zoom level
func InitParcelTilesCache() {
	// Total memory ~200MB
	// Heavily favoring lower zoom levels where tile generation is slowest
	// More aggressive falloff for high zoom levels
	// Z13-15 are the slowest, so they get the most space
	quotas := map[int]int64{
		13: 90 * 1024 * 1024,
		14: 48 * 1024 * 1024,
		15: 32 * 1024 * 1024,
		16: 16 * 1024 * 1024,
		17: 8 * 1024 * 1024,
		18: 4 * 1024 * 1024,
		19: 2 * 1024 * 1024,
	}

	ParcelTilesCache = &TileCache{
		zooms: make(map[int]*ZoomLRU),
	}

	for z, quota := range quotas {
		ParcelTilesCache.zooms[z] = NewZoomLRU(quota)
	}
}

// InitTaxTilesCache initializes in-memory caches for tax heatmap/grid and tax parcel tiles.
func InitTaxTilesCache() {
	// Grid tiles are precomputed and reused heavily; keep a moderate cache.
	TaxHeatmapTilesCache = NewStringLRU(96 * 1024 * 1024)
	// Tax parcel tiles are dynamic and can be large/hot at parcel zooms.
	TaxParcelTilesCache = NewStringLRU(160 * 1024 * 1024)
}
