package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

type parcelSearchRow struct {
	FeatureID   string  `json:"feature_id"`
	SiteAddress string  `json:"site_address"`
	OwnerName   string  `json:"owner_name"`
	CountyName  string  `json:"county_name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

// SearchParcels provides fast address autocomplete scoped to Georgia parcels.
// Prefix search is attempted first; trigram similarity is used as fallback.
func SearchParcels(c *gin.Context) {
	rawQuery := strings.TrimSpace(c.Query("q"))
	if rawQuery == "" {
		c.JSON(http.StatusOK, gin.H{"results": []parcelSearchRow{}})
		return
	}
	normalizedQuery := strings.ToLower(strings.Join(strings.Fields(rawQuery), " "))
	mode := strings.ToLower(strings.TrimSpace(c.DefaultQuery("mode", "address")))
	if mode != "address" && mode != "owner" {
		mode = "address"
	}

	limit := 10
	if v := c.Query("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			if parsed > 0 && parsed <= 20 {
				limit = parsed
			}
		}
	}

	if len(normalizedQuery) < 2 {
		c.JSON(http.StatusOK, gin.H{"results": []parcelSearchRow{}})
		return
	}

	searchColumn := "site_address"
	if mode == "owner" {
		searchColumn = "owner_name"
	}

	var results []parcelSearchRow
	prefixQuery := `
		WITH prefix_candidates AS (
			SELECT
				p.county_id,
				p.objectid,
				p.site_address,
				p.owner_name,
				p.search_lat,
				p.search_lng
			FROM parcels p
			JOIN counties co ON p.county_id = co.id
			WHERE co.state = 'GA'
			  AND p.processed IS NULL
			  AND p.objectid IS NOT NULL
			  AND p.search_lat IS NOT NULL
			  AND p.search_lng IS NOT NULL
			  AND p.%s IS NOT NULL
			  AND p.%s <> ''
			  AND lower(p.%s) LIKE ? || '%'
			ORDER BY
			  CASE
			    WHEN lower(p.%s) = ? THEN 0
			    WHEN lower(p.%s) LIKE ? || ' %' THEN 1
			    ELSE 2
			  END,
			  length(p.%s),
			  p.%s
			LIMIT ?
		)
		SELECT
			pc.county_id || '_' || pc.objectid::text AS feature_id,
			pc.site_address,
			pc.owner_name,
			co.name AS county_name,
			pc.search_lat AS lat,
			pc.search_lng AS lng
		FROM prefix_candidates pc
		JOIN counties co ON pc.county_id = co.id
	`
	prefixQuery = strings.ReplaceAll(prefixQuery, "%s", searchColumn)
	if err := db.DB.Raw(prefixQuery, normalizedQuery, normalizedQuery, normalizedQuery, limit).Scan(&results).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search parcels"})
		return
	}

	// Fuzzy fallback only when prefix hits are sparse and query is long enough.
	if len(results) < limit && len(normalizedQuery) >= 5 {
		needed := limit - len(results)
		var fuzzy []parcelSearchRow

		fuzzyQuery := `
			WITH fuzzy_candidates AS (
				SELECT
					p.county_id,
					p.objectid,
					p.site_address,
					p.owner_name,
					p.search_lat,
					p.search_lng,
					similarity(lower(p.%s), ?) AS score
				FROM parcels p
				JOIN counties co ON p.county_id = co.id
				WHERE co.state = 'GA'
				  AND p.processed IS NULL
				  AND p.objectid IS NOT NULL
				  AND p.search_lat IS NOT NULL
				  AND p.search_lng IS NOT NULL
				  AND p.%s IS NOT NULL
				  AND p.%s <> ''
				  AND lower(p.%s) % ?
				ORDER BY score DESC, p.%s
				LIMIT ?
			)
			SELECT
				fc.county_id || '_' || fc.objectid::text AS feature_id,
				fc.site_address,
				fc.owner_name,
				co.name AS county_name,
				fc.search_lat AS lat,
				fc.search_lng AS lng
			FROM fuzzy_candidates fc
			JOIN counties co ON fc.county_id = co.id
			ORDER BY fc.score DESC, fc.%s
		`
		fuzzyQuery = strings.ReplaceAll(fuzzyQuery, "%s", searchColumn)
		if err := db.DB.Raw(fuzzyQuery, normalizedQuery, normalizedQuery, needed*4).Scan(&fuzzy).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search parcels"})
			return
		}

		seen := make(map[string]struct{}, len(results))
		for _, r := range results {
			seen[r.FeatureID] = struct{}{}
		}
		for _, r := range fuzzy {
			if _, exists := seen[r.FeatureID]; exists {
				continue
			}
			results = append(results, r)
			seen[r.FeatureID] = struct{}{}
			if len(results) >= limit {
				break
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}
