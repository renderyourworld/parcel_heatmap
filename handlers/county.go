package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

// GetCountyCentroid returns county center in WGS84 for popup anchoring.
// URL format: /api/counties/{id}/centroid
func GetCountyCentroid(c *gin.Context) {
	idStr := c.Param("id")
	countyID, err := strconv.Atoi(idStr)
	if err != nil || countyID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid county id"})
		return
	}

	var row struct {
		ID    int
		Name  string
		State string
		Lat   float64
		Lng   float64
	}

	if err := db.DB.Raw(`
		SELECT
			c.id,
			c.name,
			c.state,
			ST_Y(COALESCE(c.centroid, ST_PointOnSurface(c.boundary))) AS lat,
			ST_X(COALESCE(c.centroid, ST_PointOnSurface(c.boundary))) AS lng
		FROM counties c
		WHERE c.id = ?
		LIMIT 1
	`, countyID).Scan(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch county centroid"})
		return
	}

	if row.ID == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "county not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":    row.ID,
		"name":  row.Name,
		"state": row.State,
		"lat":   row.Lat,
		"lng":   row.Lng,
	})
}
