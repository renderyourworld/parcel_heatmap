package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

// ParcelGeoJSON represents a parcel record with precomputed GeoJSON geometry.
// The geojson_data field contains the raw GeoJSON string from the database.
type ParcelGeoJSON struct {
	ID             uint64  `json:"id"`
	SourceParcelID string  `json:"source_parcel_id"`
	OwnerName      string  `json:"owner_name"`
	Address        string  `json:"address"`
	Acreage        float64 `json:"acreage"`
	GeoJSONData    string  `gorm:"column:geojson_data" json:"geojson_data"`
}

// Handles API requests for parcels within the current map view.
func GetVisibleParcels(c *gin.Context) {
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

	// Construct the ST_Intersects query
	sqlQuery := `
		SELECT 
			id, source_parcel_id, owner_name, address, acreage, ST_AsGeoJSON(geometry) AS geojson_data
		FROM 
			parcels
		WHERE 
			ST_Intersects(
				geometry,
				ST_MakeEnvelope($1, $2, $3, $4, 4326)
			);
	`

	// Will contain the GeoJSON string for each parcel
	var results []ParcelGeoJSON

	// Pass the bounds to the query
	tx := db.DB.Raw(sqlQuery, minX, minY, maxX, maxY).Scan(&results)
	if tx.Error != nil {
		log.Printf("PostGIS query error: %v", tx.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed"})
		return
	}

	// Return the results as JSON
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"count":   len(results),
		"parcels": results,
	})
}
