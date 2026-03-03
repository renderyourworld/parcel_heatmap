package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

// GetParcelPreviews returns simplified parcel geometry previews for a batch of feature IDs.
// URL format: POST /api/parcels/previews  body: { "feature_ids": ["671_123", ...] }
func GetParcelPreviews(c *gin.Context) {
	var req struct {
		FeatureIDs []string `json:"feature_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.FeatureIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"rows": []gin.H{}})
		return
	}
	if len(req.FeatureIDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "feature_ids exceeds max of 100"})
		return
	}

	type parsedID struct {
		FeatureID string
		CountyID  int
		ObjectID  int64
	}
	parsed := make([]parsedID, 0, len(req.FeatureIDs))
	for _, id := range req.FeatureIDs {
		raw := strings.TrimSpace(id)
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, "_", 2)
		if len(parts) != 2 {
			continue
		}
		countyID, err := strconv.Atoi(parts[0])
		if err != nil || countyID <= 0 {
			continue
		}
		objectID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || objectID <= 0 {
			continue
		}
		parsed = append(parsed, parsedID{
			FeatureID: raw,
			CountyID:  countyID,
			ObjectID:  objectID,
		})
	}
	if len(parsed) == 0 {
		c.JSON(http.StatusOK, gin.H{"rows": []gin.H{}})
		return
	}

	var values []string
	args := make([]interface{}, 0, len(parsed)*3)
	for _, p := range parsed {
		values = append(values, "(?::text, ?::integer, ?::bigint)")
		args = append(args, p.FeatureID, p.CountyID, p.ObjectID)
	}

	query := fmt.Sprintf(`
		WITH req(feature_id, county_id, objectid) AS (
			VALUES %s
		)
		SELECT
			r.feature_id,
			ST_AsGeoJSON(
				ST_SimplifyPreserveTopology(p.geometry, 0.00001),
				5
			) AS geometry_geojson
		FROM req r
		JOIN parcels p
		  ON p.county_id = r.county_id
		 AND p.objectid = r.objectid
		WHERE p.processed IS NULL
		  AND p.geometry IS NOT NULL
	`, strings.Join(values, ","))

	var rows []struct {
		FeatureID       string         `gorm:"column:feature_id"`
		GeometryGeoJSON sql.NullString `gorm:"column:geometry_geojson"`
	}
	if err := db.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch parcel previews"})
		return
	}

	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		if !r.GeometryGeoJSON.Valid || strings.TrimSpace(r.GeometryGeoJSON.String) == "" {
			continue
		}
		out = append(out, gin.H{
			"feature_id": r.FeatureID,
			"geometry":   r.GeometryGeoJSON.String,
		})
	}

	c.JSON(http.StatusOK, gin.H{"rows": out})
}
