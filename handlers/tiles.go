package handlers

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/tiles"
)

// GetVectorTile get pre-generated MVT tiles from the database
// Tiles are stored as gzipped PBF binary data in the tiles table
// URL format: /api/tiles/{z}/{x}/{y}.pbf
func GetVectorTile(c *gin.Context) {
	// Parse tile coordinates from URL
	z, err := strconv.Atoi(c.Param("z"))
	if err != nil {
		log.Printf("ERROR: Invalid z param: %s", c.Param("z"))
		c.Status(http.StatusBadRequest)
		return
	}

	x, err := strconv.Atoi(c.Param("x"))
	if err != nil {
		log.Printf("ERROR: Invalid x param: %s", c.Param("x"))
		c.Status(http.StatusBadRequest)
		return
	}

	y, err := strconv.Atoi(c.Param("y"))
	if err != nil {
		log.Printf("ERROR: Invalid y param: %s, full URL: %s", c.Param("y"), c.Request.URL.Path)
		c.Status(http.StatusBadRequest)
		return
	}

	// Validate zoom range (parcels are pre-generated for zoom 13-19)
	if z < 13 || z > 19 {
		c.Status(http.StatusNoContent) // Return empty tile for out of range
		return
	}

	// Check Cache first
	if tiles.ParcelTilesCache != nil {
		if cachedData, hit := tiles.ParcelTilesCache.Get(z, x, y); hit {
			c.Header("Content-Type", "application/x-protobuf")
			c.Header("Cache-Control", "public, max-age=2592000, immutable")
			c.Header("X-Cache", "HIT")
			c.Data(http.StatusOK, "application/x-protobuf", cachedData)
			return
		}
	}

	// Get tile from database if there is a cache miss
	var tileData []byte
	row := db.DB.Raw(`
		SELECT data 
		FROM tiles 
		WHERE z = ? AND x = ? AND y = ? AND layer = 'parcels'
	`, z, x, y).Row()

	err = row.Scan(&tileData)
	if err != nil {
		// Check if it's a "no rows" error (tile doesn't exist)
		if err.Error() == "sql: no rows in result set" {
			c.Status(http.StatusNoContent)
			return
		}
		log.Printf("ERROR: Failed to query tile %d/%d/%d: %v", z, x, y, err)
		c.Status(http.StatusInternalServerError)
		return
	}

	// Return empty tile if no data found (no parcels in this area)
	if len(tileData) == 0 {
		c.Status(http.StatusNoContent)
		return
	}

	// Decompress the gzipped tile data (stored compressed in database)
	// MapLibre wants uncompressed MVT data
	reader, err := gzip.NewReader(bytes.NewReader(tileData))
	if err != nil {
		log.Printf("ERROR: Failed to decompress tile %d/%d/%d: %v", z, x, y, err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	uncompressed, err := io.ReadAll(reader)
	if err != nil {
		log.Printf("ERROR: Failed to read decompressed tile %d/%d/%d: %v", z, x, y, err)
		c.Status(http.StatusInternalServerError)
		return
	}

	// Debug: Log tile info
	if len(uncompressed) == 0 {
		log.Printf("WARNING: Tile %d/%d/%d decompressed to 0 bytes (was %d bytes compressed)", z, x, y, len(tileData))
		c.Status(http.StatusNoContent)
		return
	}

	// Put in Cache
	if tiles.ParcelTilesCache != nil && len(uncompressed) > 0 {
		tiles.ParcelTilesCache.Put(z, x, y, uncompressed)
	}

	// Return uncompressed MVT tile
	c.Header("Content-Type", "application/x-protobuf")
	c.Header("Cache-Control", "public, max-age=2592000, immutable") // Cache for 30 days, tiles never change
	c.Header("X-Cache", "MISS")
	c.Data(http.StatusOK, "application/x-protobuf", uncompressed)
}

// ServePMTilesWithCache serves georgia.pmtiles with a map zoom-based LRU cache
func ServePMTilesWithCache(c *gin.Context) {
	// Set basic headers for all responses
	c.Header("Content-Type", "application/vnd.pmtiles")
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	c.Header("Accept-Ranges", "bytes")

	// Get file stats
	fi, err := os.Stat("./tiles/georgia.pmtiles")
	if err != nil {
		log.Printf("ERROR: Failed to stat pmtiles file: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	// Cache Lookup
	if tiles.PMTilesCache != nil {
		if cachedData, hit := tiles.PMTilesCache.Get(rangeHeader); hit {
			// Get range values for the header
			start, end, _ := ParseRangeHeader(rangeHeader, fi.Size())

			c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fi.Size()))
			c.Header("X-Cache", "HIT")
			c.Data(http.StatusPartialContent, "application/vnd.pmtiles", cachedData)
			return
		}
	}

	// Cache Miss: Read from disk
	file, err := os.Open("./tiles/georgia.pmtiles")
	if err != nil {
		log.Printf("ERROR: Failed to open pmtiles file: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer file.Close()

	start, end, err := ParseRangeHeader(rangeHeader, fi.Size())
	if err != nil {
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	length := end - start + 1
	if length <= 0 {
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	data := make([]byte, length)
	_, err = file.ReadAt(data, start)
	if err != nil && err != io.EOF {
		log.Printf("ERROR: Failed to read pmtiles range: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	// Add to cache
	if tiles.PMTilesCache != nil {
		atomic.AddUint64(&tiles.PMTilesSize, uint64(len(data)))
		tiles.PMTilesCache.Add(rangeHeader, data)
	}

	// Partial content response
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fi.Size()))
	c.Header("X-Cache", "MISS")
	c.Data(http.StatusPartialContent, "application/vnd.pmtiles", data)
}

// ParseRangeHeader parses a standard HTTP range header
func ParseRangeHeader(rangeHeader string, fileSize int64) (int64, int64, error) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range prefix")
	}

	parts := strings.Split(rangeHeader[6:], "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start byte: %v", err)
	}

	var end int64
	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end byte: %v", err)
		}
	}

	if start < 0 || end < start || end >= fileSize {
		return 0, 0, fmt.Errorf("range out of bounds")
	}

	return start, end, nil
}

// GetCacheStats returns the current memory usage of the tiles caches
func GetCacheStats(c *gin.Context) {
	stats := ""
	if tiles.ParcelTilesCache != nil {
		stats += tiles.ParcelTilesCache.Stats()
	} else {
		stats += "Tile Cache: Not initialized\n"
	}

	if tiles.PMTilesCache != nil {
		size := atomic.LoadUint64(&tiles.PMTilesSize)
		stats += fmt.Sprintf("\nPMTiles Cache Stats:\n  Entries: %d\n  Cache Size: %.2f MB\n",
			tiles.PMTilesCache.Len(), float64(size)/1024/1024)
	} else {
		stats += "\nPMTiles Cache: Not initialized\n"
	}

	c.String(http.StatusOK, stats)
}
