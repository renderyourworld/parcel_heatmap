package handlers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

type parcelSearchRow struct {
	FeatureID      string  `json:"feature_id"`
	SiteAddress    string  `json:"site_address"`
	DisplayAddress string  `json:"display_address"`
	OwnerName      string  `json:"owner_name"`
	CountyName     string  `json:"county_name"`
	MailingCity    string  `json:"mailing_city"`
	MailingZip5    string  `json:"mailing_zip5"`
	Lat            float64 `json:"lat"`
	Lng            float64 `json:"lng"`
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

	zipToken := extractZipToken(normalizedQuery)
	cityToken := extractCityToken(normalizedQuery)
	streetQuery := extractStreetQuery(normalizedQuery)
	if streetQuery == "" {
		streetQuery = normalizedQuery
	}

	var results []parcelSearchRow
	if mode == "address" {
		prefixQuery := `
			WITH prefix_candidates AS (
				SELECT
					ps.county_id,
					ps.objectid,
					COALESCE(ps.site_address, '') AS site_address,
					COALESCE(ps.display_address, COALESCE(ps.site_address, '')) AS display_address,
					COALESCE(ps.mailing_city, '') AS mailing_city,
					COALESCE(ps.mailing_zip5, '') AS mailing_zip5,
					ps.lat,
					ps.lng
				FROM parcel_search ps
				WHERE ps.site_address_norm IS NOT NULL
				  AND ps.site_address_norm <> ''
				  AND lower(ps.site_address_norm) LIKE ? || '%'
				ORDER BY
				  CASE
				    WHEN lower(ps.site_address_norm) = ? THEN 0
				    WHEN lower(ps.site_address_norm) LIKE ? || '%' THEN 1
				    WHEN lower(ps.display_address) = ? THEN 2
				    WHEN lower(ps.display_address) LIKE ? || '%' THEN 3
				    ELSE 4
				  END,
				  CASE
				    WHEN ? <> '' AND ps.mailing_zip5 = ? THEN 0
				    ELSE 1
				  END,
				  CASE
				    WHEN ? <> '' AND lower(ps.mailing_city) = ? THEN 0
				    WHEN ? <> '' AND lower(ps.mailing_city) LIKE ? || '%' THEN 1
				    ELSE 2
				  END,
				  length(COALESCE(ps.display_address, ps.site_address, '')),
				  COALESCE(ps.display_address, ps.site_address, '')
				LIMIT ?
			)
			SELECT
				pc.county_id || '_' || pc.objectid::text AS feature_id,
				pc.site_address,
				pc.display_address,
				'' AS owner_name,
				co.name AS county_name,
				pc.mailing_city,
				pc.mailing_zip5,
				pc.lat,
				pc.lng
			FROM prefix_candidates pc
			JOIN counties co ON pc.county_id = co.id
		`

		if err := db.DB.Raw(
			prefixQuery,
			streetQuery,
			streetQuery,
			streetQuery,
			normalizedQuery,
			normalizedQuery,
			zipToken,
			zipToken,
			cityToken,
			cityToken,
			cityToken,
			cityToken,
			limit,
		).Scan(&results).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search parcels"})
			return
		}

		if len(results) < limit && len(normalizedQuery) >= 5 {
			needed := limit - len(results)
			var fuzzy []parcelSearchRow

			fuzzyQuery := `
				WITH fuzzy_candidates AS (
					SELECT
						ps.county_id,
						ps.objectid,
						COALESCE(ps.site_address, '') AS site_address,
						COALESCE(ps.display_address, COALESCE(ps.site_address, '')) AS display_address,
						COALESCE(ps.mailing_city, '') AS mailing_city,
						COALESCE(ps.mailing_zip5, '') AS mailing_zip5,
						ps.lat,
						ps.lng,
						GREATEST(
							similarity(lower(COALESCE(ps.site_address_norm, '')), ?),
							similarity(lower(COALESCE(ps.display_address, '')), ?)
						) AS score
					FROM parcel_search ps
					WHERE ps.site_address_norm IS NOT NULL
					  AND ps.site_address_norm <> ''
					  AND lower(ps.site_address_norm) % ?
					ORDER BY score DESC
					LIMIT ?
				)
				SELECT
					fc.county_id || '_' || fc.objectid::text AS feature_id,
					fc.site_address,
					fc.display_address,
					'' AS owner_name,
					co.name AS county_name,
					fc.mailing_city,
					fc.mailing_zip5,
					fc.lat,
					fc.lng
				FROM fuzzy_candidates fc
				JOIN counties co ON fc.county_id = co.id
				ORDER BY fc.score DESC,
					CASE
						WHEN ? <> '' AND fc.mailing_zip5 = ? THEN 0
						ELSE 1
					END,
					CASE
						WHEN ? <> '' AND lower(fc.mailing_city) = ? THEN 0
						WHEN ? <> '' AND lower(fc.mailing_city) LIKE ? || '%' THEN 1
						ELSE 2
					END
			`

			if err := db.DB.Raw(
				fuzzyQuery,
				streetQuery,
				normalizedQuery,
				streetQuery,
				needed*4,
				zipToken,
				zipToken,
				cityToken,
				cityToken,
				cityToken,
				cityToken,
			).Scan(&fuzzy).Error; err != nil {
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
	} else {
		prefixQuery := `
			WITH prefix_candidates AS (
				SELECT
					p.county_id,
					p.objectid,
					COALESCE(p.site_address, '') AS site_address,
					COALESCE(p.site_address, '') AS display_address,
					COALESCE(p.owner_name, '') AS owner_name,
					p.search_lat,
					p.search_lng
				FROM parcels p
				JOIN counties co ON p.county_id = co.id
				WHERE co.state = 'GA'
				  AND p.processed IS NULL
				  AND p.objectid IS NOT NULL
				  AND p.search_lat IS NOT NULL
				  AND p.search_lng IS NOT NULL
				  AND p.owner_name IS NOT NULL
				  AND p.owner_name <> ''
				  AND lower(p.owner_name) LIKE ? || '%'
				ORDER BY
				  CASE
				    WHEN lower(p.owner_name) = ? THEN 0
				    WHEN lower(p.owner_name) LIKE ? || ' %' THEN 1
				    ELSE 2
				  END,
				  length(p.owner_name),
				  p.owner_name
				LIMIT ?
			)
			SELECT
				pc.county_id || '_' || pc.objectid::text AS feature_id,
				pc.site_address,
				pc.display_address,
				pc.owner_name,
				co.name AS county_name,
				'' AS mailing_city,
				'' AS mailing_zip5,
				pc.search_lat AS lat,
				pc.search_lng AS lng
			FROM prefix_candidates pc
			JOIN counties co ON pc.county_id = co.id
		`
		if err := db.DB.Raw(prefixQuery, normalizedQuery, normalizedQuery, normalizedQuery, limit).Scan(&results).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search parcels"})
			return
		}

		if len(results) < limit && len(normalizedQuery) >= 5 {
			needed := limit - len(results)
			var fuzzy []parcelSearchRow

			fuzzyQuery := `
				WITH fuzzy_candidates AS (
					SELECT
						p.county_id,
						p.objectid,
						COALESCE(p.site_address, '') AS site_address,
						COALESCE(p.site_address, '') AS display_address,
						COALESCE(p.owner_name, '') AS owner_name,
						p.search_lat,
						p.search_lng,
						similarity(lower(p.owner_name), ?) AS score
					FROM parcels p
					JOIN counties co ON p.county_id = co.id
					WHERE co.state = 'GA'
					  AND p.processed IS NULL
					  AND p.objectid IS NOT NULL
					  AND p.search_lat IS NOT NULL
					  AND p.search_lng IS NOT NULL
					  AND p.owner_name IS NOT NULL
					  AND p.owner_name <> ''
					  AND lower(p.owner_name) % ?
					ORDER BY score DESC, p.owner_name
					LIMIT ?
				)
				SELECT
					fc.county_id || '_' || fc.objectid::text AS feature_id,
					fc.site_address,
					fc.display_address,
					fc.owner_name,
					co.name AS county_name,
					'' AS mailing_city,
					'' AS mailing_zip5,
					fc.search_lat AS lat,
					fc.search_lng AS lng
				FROM fuzzy_candidates fc
				JOIN counties co ON fc.county_id = co.id
				ORDER BY fc.score DESC, fc.owner_name
			`

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
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

var zipTokenPattern = regexp.MustCompile(`\b(\d{5})(?:-\d{4})?\b`)
var nonCityPattern = regexp.MustCompile(`[^a-z\s]`)

func extractZipToken(q string) string {
	match := zipTokenPattern.FindStringSubmatch(q)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func extractStreetQuery(q string) string {
	withoutZip := strings.TrimSpace(zipTokenPattern.ReplaceAllString(q, " "))
	parts := strings.Split(withoutZip, ",")
	street := strings.TrimSpace(parts[0])
	return strings.Join(strings.Fields(street), " ")
}

func extractCityToken(q string) string {
	parts := strings.Split(q, ",")
	if len(parts) < 2 {
		return ""
	}
	city := strings.TrimSpace(parts[1])
	city = strings.ReplaceAll(city, "ga", " ")
	city = nonCityPattern.ReplaceAllString(city, " ")
	city = strings.Join(strings.Fields(city), " ")
	return city
}
