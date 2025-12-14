package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

// GetParcels returns parcel GeoJSON for the specified bounding box
// Only returns successfully processed parcels (processed IS NULL or processed IS NOT FALSE)
func GetParcels(c *gin.Context) {
	// Get the map bounds from query parameters
	minX, err := strconv.ParseFloat(c.Query("minX"), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or invalid minX parameter"})
		return
	}

	minY, err := strconv.ParseFloat(c.Query("minY"), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or invalid minY parameter"})
		return
	}

	maxX, err := strconv.ParseFloat(c.Query("maxX"), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or invalid maxX parameter"})
		return
	}

	maxY, err := strconv.ParseFloat(c.Query("maxY"), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or invalid maxY parameter"})
		return
	}

	// Execute query
	sqlQuery := `
		SELECT jsonb_build_object(
			'type', 'FeatureCollection',
			'features', COALESCE(jsonb_agg(boundary_geojson), '[]'::jsonb)
		)::text AS geojson
		FROM parcels
		WHERE ST_Intersects(geometry, ST_MakeEnvelope($1, $2, $3, $4, 4326))
		AND (processed IS NULL OR processed IS NOT FALSE)
	`

	var geojsonResult string
	if err := db.DB.Raw(sqlQuery, minX, minY, maxX, maxY).Scan(&geojsonResult).Error; err != nil {
		log.Printf("PostGIS query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed"})
		return
	}

	// Return GeoJSON with proper content type
	c.Header("Content-Type", "application/vnd.geo+json")
	c.String(http.StatusOK, geojsonResult)
}
