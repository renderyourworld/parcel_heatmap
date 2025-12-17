package handlers

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
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

	// Query pre-generated tile from database
	var tileData []byte
	row := db.DB.Raw(`
		SELECT data 
		FROM tiles 
		WHERE z = ? AND x = ? AND y = ? AND layer = 'parcels'
		LIMIT 1
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

	// Return uncompressed MVT tile
	c.Header("Content-Type", "application/x-protobuf")
	c.Header("Cache-Control", "public, max-age=86400") // Cache for 24 hours
	c.Data(http.StatusOK, "application/x-protobuf", uncompressed)
}
