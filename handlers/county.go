package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

// Returns all county boundaries as a GeoJSON FeatureCollection.
// Supports zoom-aware detail switching via the "detail" query parameter:
// - "simplified": Returns simplified geometry (~15-20ms) for low-zoom panning
// - "full": Returns full-detail geometry (~2ms cached, ~120ms cold) for high-zoom viewing
//
// The full-detail response is automatically gzipped and cached in-memory to eliminate
// repeated expensive database queries. The simplified response uses a precomputed
// JSONB column in the database for fast geometry serialization.
func GetCountyBoundaries(c *gin.Context) {
	var geoJSONString string
	var query string

	// Determine which geometry column and filtering to use
	detail := c.DefaultQuery("detail", "simplified") // Default to 'simplified'

	// For simplified, use precomputed boundary_simplified_geojson column
	query = `
		SELECT jsonb_build_object(
			'type', 'FeatureCollection',
			'features', jsonb_agg(t.boundary_simplified_geojson)
		)::text AS geojson
		FROM counties AS t;
	`

	// If detail is 'full', check cache first before querying DB.
	// Cache stores all 159 counties' full boundaries (GZIP middleware handles compression)
	if detail == "full" {
		cacheKey := "all_counties|full"

		// Try to get from cache (store uncompressed, let middleware compress)
		if jsonData, found := countyCache.Get(cacheKey); found {
			log.Printf("[Cache HIT] Serving all full county boundaries from cache")
			c.Data(http.StatusOK, "application/vnd.geo+json", jsonData)
			return
		}

		// Cache miss: proceed with DB query for all counties
		log.Printf("[Cache MISS] Full county boundaries not in cache; querying DB for all counties")

		// For full detail, return ALL 159 counties (cached as a single response)
		query = `
			SELECT jsonb_build_object(
				'type', 'FeatureCollection',
				'features', jsonb_agg(t.boundary_geojson)
			)::text AS geojson
			FROM counties AS t;
		`
	}

	// Scan into a plain string to avoid struct-mapping issues.
	err := db.DB.Raw(query).Scan(&geoJSONString).Error
	if err != nil {
		log.Printf("Database error fetching county GeoJSON: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch county boundaries: " + err.Error()})
		return
	}

	// If this is a full-detail request, cache the uncompressed JSON
	// GZIP middleware will handle compression automatically
	if detail == "full" {
		cacheKey := "all_counties|full"
		countyCache.Set(cacheKey, []byte(geoJSONString))
		log.Printf("[Cache STORE] Cached all full county boundaries (%d bytes uncompressed)", len(geoJSONString))
	}

	c.Data(http.StatusOK, "application/vnd.geo+json", []byte(geoJSONString))
}
