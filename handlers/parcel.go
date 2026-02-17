package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

// GetParcelDetails returns full parcel details for popup hydration.
// URL format: /api/parcels/{feature_id} where feature_id is "{county_id}_{objectid}".
func GetParcelDetails(c *gin.Context) {
	featureID := c.Param("feature_id")
	parts := strings.SplitN(featureID, "_", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid feature_id format"})
		return
	}

	countyID, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid county_id in feature_id"})
		return
	}

	objectID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid objectid in feature_id"})
		return
	}

	var details struct {
		FeatureID      string
		ParcelID       sql.NullString
		SiteAddress    sql.NullString
		OwnerName      sql.NullString
		OwnerAddress   sql.NullString
		Acres          sql.NullFloat64
		Classification sql.NullString
		Category       sql.NullString
		TaxDistrict    sql.NullString
		SearchLat      sql.NullFloat64
		SearchLng      sql.NullFloat64
		UpdatedAt      sql.NullTime
	}

	row := db.DB.Raw(`
		SELECT
			p.county_id || '_' || p.objectid AS feature_id,
			p.parcel_id,
			p.site_address,
			p.owner_name,
			p.owner_address,
			p.acres::float8 AS acres,
			p.classification,
			cc.category,
			p.tax_district,
			p.search_lat,
			p.search_lng,
			p.updated_at
		FROM parcels p
		LEFT JOIN parcel_class_codes cc
			ON p.county_id = cc.county_id AND p.classification = cc.code
		WHERE p.county_id = ? AND p.objectid = ? AND p.processed IS NULL
		LIMIT 1
	`, countyID, objectID).Row()

	if err := row.Scan(
		&details.FeatureID,
		&details.ParcelID,
		&details.SiteAddress,
		&details.OwnerName,
		&details.OwnerAddress,
		&details.Acres,
		&details.Classification,
		&details.Category,
		&details.TaxDistrict,
		&details.SearchLat,
		&details.SearchLng,
		&details.UpdatedAt,
	); err != nil {
		if err.Error() == "sql: no rows in result set" {
			c.JSON(http.StatusNotFound, gin.H{"error": "parcel not found"})
			return
		}
		log.Printf("ERROR: Failed parcel lookup for feature_id=%s: %v", featureID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch parcel details"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"feature_id":     details.FeatureID,
		"parcel_id":      nullString(details.ParcelID),
		"site_address":   nullString(details.SiteAddress),
		"owner_name":     nullString(details.OwnerName),
		"owner_address":  nullString(details.OwnerAddress),
		"acres":          nullFloat(details.Acres),
		"classification": nullString(details.Classification),
		"category":       nullString(details.Category),
		"tax_district":   nullString(details.TaxDistrict),
		"lat":            nullFloat(details.SearchLat),
		"lng":            nullFloat(details.SearchLng),
		"updated_at":     nullTime(details.UpdatedAt),
	})
}

func nullString(v sql.NullString) interface{} {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullFloat(v sql.NullFloat64) interface{} {
	if v.Valid {
		return v.Float64
	}
	return nil
}

func nullTime(v sql.NullTime) interface{} {
	if v.Valid {
		return v.Time
	}
	return nil
}
