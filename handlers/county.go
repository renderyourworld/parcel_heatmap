package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
)

// Package-level variables to store preloaded county boundary data
var (
	simplifiedCountyData           []byte
	fullCountyData                 []byte
	simplifiedCountyDataCompressed []byte
	fullCountyDataCompressed       []byte
)

// LoadCountyBoundaries preloads simplified and full county boundary data
// from the database and stores them in memory.
func LoadCountyBoundaries(database *gorm.DB) error {
	log.Println("Loading county boundaries from database...")

	// Load simplified boundary data
	var simplifiedGeoJSON string
	simplifiedQuery := `
		SELECT jsonb_build_object(
			'type', 'FeatureCollection',
			'features', jsonb_agg(t.boundary_simplified_geojson)
		)::text AS geojson
		FROM counties AS t;
	`
	if err := database.Raw(simplifiedQuery).Scan(&simplifiedGeoJSON).Error; err != nil {
		return err
	}
	simplifiedCountyData = []byte(simplifiedGeoJSON)
	log.Printf("Loaded simplified county boundaries")

	// Compress simplified data
	simplifiedCompressed, err := utils.Gzip(simplifiedCountyData)
	if err != nil {
		return err
	}
	simplifiedCountyDataCompressed = simplifiedCompressed

	// Load full boundary data
	var fullGeoJSON string
	fullQuery := `
		SELECT jsonb_build_object(
			'type', 'FeatureCollection',
			'features', jsonb_agg(t.boundary_geojson)
		)::text AS geojson
		FROM counties AS t;
	`
	if err := database.Raw(fullQuery).Scan(&fullGeoJSON).Error; err != nil {
		return err
	}
	fullCountyData = []byte(fullGeoJSON)
	log.Printf("Loaded full county boundaries")

	// Compress full data
	fullCompressed, err := utils.Gzip(fullCountyData)
	if err != nil {
		return err
	}
	fullCountyDataCompressed = fullCompressed

	return nil
}

// GetSimplifiedCountyBoundaries returns preloaded simplified county boundaries.
func GetSimplifiedCountyBoundaries(c *gin.Context) {
	c.Header("Content-Encoding", "gzip")
	c.Data(http.StatusOK, "application/vnd.geo+json", simplifiedCountyDataCompressed)
}

// GetFullCountyBoundaries returns preloaded full-detail county boundaries.
func GetFullCountyBoundaries(c *gin.Context) {
	c.Header("Content-Encoding", "gzip")
	c.Data(http.StatusOK, "application/vnd.geo+json", fullCountyDataCompressed)
}
