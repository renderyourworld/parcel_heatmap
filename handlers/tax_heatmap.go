package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/tiles"
	"github.com/renderyourworld/parcel_heatmap/utils"
)

const taxHeatmapStatsTTL = 6 * time.Hour

type taxHeatmapStatsResponse struct {
	CountyID  uint16   `json:"county_id"`
	Year      int      `json:"year"`
	MinValue  *float64 `json:"min_value"`
	MaxValue  *float64 `json:"max_value"`
	AvgValue  *float64 `json:"avg_value"`
	P10       *float64 `json:"p10"`
	P50       *float64 `json:"p50"`
	P90       *float64 `json:"p90"`
	Count     int64    `json:"count"`
	ValueType string   `json:"value_type"`
}

type cachedTaxHeatmapStats struct {
	Data      taxHeatmapStatsResponse
	ExpiresAt time.Time
}

var taxHeatmapStatsCache = struct {
	mu   sync.RWMutex
	data map[string]cachedTaxHeatmapStats
}{
	data: make(map[string]cachedTaxHeatmapStats),
}

func taxHeatmapStatsCacheKey(countyID uint16, year int) string {
	return fmt.Sprintf("%d:%d", countyID, year)
}

func setTaxHeatmapStatsHeaders(c *gin.Context, cacheStatus string) {
	c.Header("Cache-Control", "public, max-age=900, stale-while-revalidate=3600")
	c.Header("X-Cache", cacheStatus)
}

func writeTaxHeatmapStatsResponse(c *gin.Context, payload taxHeatmapStatsResponse, cacheStatus string) {
	setTaxHeatmapStatsHeaders(c, cacheStatus)
	c.JSON(http.StatusOK, payload)
}

func loadTaxHeatmapStatsFromDB(countyID uint16, year int) (taxHeatmapStatsResponse, error) {
	var out struct {
		MinValue *float64 `json:"min_value"`
		MaxValue *float64 `json:"max_value"`
		AvgValue *float64 `json:"avg_value"`
		P10      *float64 `json:"p10"`
		P50      *float64 `json:"p50"`
		P90      *float64 `json:"p90"`
		Count    int64    `json:"count"`
	}

	err := db.DB.Raw(`
		WITH vals AS (
			SELECT pt.tax_amount::double precision AS tax_amount
			FROM parcel_taxes pt
			JOIN parcels p ON p.id = pt.parcel_id
			WHERE p.county_id = ?
			  AND pt.tax_year = ?
			  AND pt.tax_amount IS NOT NULL
			  AND pt.tax_amount > 0
		)
		SELECT
			MIN(tax_amount) AS min_value,
			MAX(tax_amount) AS max_value,
			AVG(tax_amount) AS avg_value,
			percentile_cont(0.10) WITHIN GROUP (ORDER BY tax_amount) AS p10,
			percentile_cont(0.50) WITHIN GROUP (ORDER BY tax_amount) AS p50,
			percentile_cont(0.90) WITHIN GROUP (ORDER BY tax_amount) AS p90,
			COUNT(*)::bigint AS count
		FROM vals
	`, countyID, year).Scan(&out).Error
	if err != nil {
		return taxHeatmapStatsResponse{}, err
	}

	return taxHeatmapStatsResponse{
		CountyID:  countyID,
		Year:      year,
		MinValue:  out.MinValue,
		MaxValue:  out.MaxValue,
		AvgValue:  out.AvgValue,
		P10:       out.P10,
		P50:       out.P50,
		P90:       out.P90,
		Count:     out.Count,
		ValueType: "tax_amount",
	}, nil
}

// PreloadTaxHeatmapStatsForCounty computes and stores tax heatmap stats for all
// available years in-memory for a county during server startup.
func PreloadTaxHeatmapStatsForCounty(countyID uint16) error {
	var years []int
	if err := db.DB.Raw(`
		SELECT DISTINCT pt.tax_year
		FROM parcel_taxes pt
		JOIN parcels p ON p.id = pt.parcel_id
		WHERE p.county_id = ?
		ORDER BY pt.tax_year
	`, countyID).Scan(&years).Error; err != nil {
		return fmt.Errorf("failed loading tax years for county %d: %w", countyID, err)
	}
	if len(years) == 0 {
		return nil
	}

	now := time.Now()
	taxHeatmapStatsCache.mu.Lock()
	for _, year := range years {
		payload, err := loadTaxHeatmapStatsFromDB(countyID, year)
		if err != nil {
			taxHeatmapStatsCache.mu.Unlock()
			return fmt.Errorf("failed preloading stats county=%d year=%d: %w", countyID, year, err)
		}
		key := taxHeatmapStatsCacheKey(countyID, year)
		taxHeatmapStatsCache.data[key] = cachedTaxHeatmapStats{
			Data:      payload,
			ExpiresAt: now.Add(taxHeatmapStatsTTL),
		}
	}
	taxHeatmapStatsCache.mu.Unlock()
	return nil
}

func parseYearParam(c *gin.Context) (int, bool) {
	yearParam := c.Query("year")
	if yearParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing year query param"})
		return 0, false
	}

	year, err := strconv.Atoi(yearParam)
	if err != nil || year < 1900 || year > 2100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid year"})
		return 0, false
	}
	return year, true
}

func parseCountyID(c *gin.Context) uint16 {
	countyID := uint16(671) // Forsyth default for v1
	if raw := c.Query("county_id"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 65535 {
			countyID = uint16(parsed)
		}
	}
	return countyID
}

func taxParcelLayerName(countyID uint16, year int) string {
	return fmt.Sprintf("tax_parcels_%d_%d", countyID, year)
}

// GetTaxHeatmapTile serves pre-generated tax heatmap tiles for a specific year.
// URL format: /api/tiles/tax-heatmap/{z}/{x}/{y}?year=2024
func GetTaxHeatmapTile(c *gin.Context) {
	z, err := strconv.Atoi(c.Param("z"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	x, err := strconv.Atoi(c.Param("x"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	y, err := strconv.Atoi(c.Param("y"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if z < 6 || z > 16 {
		c.Status(http.StatusNoContent)
		return
	}

	year, ok := parseYearParam(c)
	if !ok {
		return
	}
	layer := "tax_heatmap_" + strconv.Itoa(year)
	cacheKey := fmt.Sprintf("%d:%d:%d:%d", year, z, x, y)

	if tiles.TaxHeatmapTilesCache != nil {
		if cachedData, hit := tiles.TaxHeatmapTilesCache.Get(cacheKey); hit {
			c.Header("Content-Type", "application/x-protobuf")
			c.Header("Content-Encoding", "gzip")
			c.Header("Cache-Control", "public, max-age=2592000, immutable")
			c.Header("X-Cache", "HIT")
			c.Data(http.StatusOK, "application/x-protobuf", cachedData)
			return
		}
	}

	var tileData []byte
	row := db.DB.Raw(`
		SELECT data
		FROM tiles
		WHERE z = ? AND x = ? AND y = ? AND layer = ?
	`, z, x, y, layer).Row()

	err = row.Scan(&tileData)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			c.Status(http.StatusNoContent)
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}

	if len(tileData) == 0 {
		c.Status(http.StatusNoContent)
		return
	}

	if tiles.TaxHeatmapTilesCache != nil {
		tiles.TaxHeatmapTilesCache.Put(cacheKey, tileData)
	}

	c.Header("Content-Type", "application/x-protobuf")
	c.Header("Content-Encoding", "gzip")
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	c.Header("X-Cache", "MISS")
	c.Data(http.StatusOK, "application/x-protobuf", tileData)
}

// GetTaxParcelTile renders parcel polygons with tax amount per selected year.
// URL format: /api/tiles/tax-parcels/{z}/{x}/{y}?year=2024
func GetTaxParcelTile(c *gin.Context) {
	z, err := strconv.Atoi(c.Param("z"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	x, err := strconv.Atoi(c.Param("x"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	y, err := strconv.Atoi(c.Param("y"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if z < 13 || z > 19 {
		c.Status(http.StatusNoContent)
		return
	}

	year, ok := parseYearParam(c)
	if !ok {
		return
	}
	countyID := parseCountyID(c)
	precomputedLayer := taxParcelLayerName(countyID, year)
	cacheKey := fmt.Sprintf("%d:%d:%d:%d:%d", countyID, year, z, x, y)

	if tiles.TaxParcelTilesCache != nil {
		if cachedData, hit := tiles.TaxParcelTilesCache.Get(cacheKey); hit {
			c.Header("Content-Type", "application/x-protobuf")
			c.Header("Content-Encoding", "gzip")
			c.Header("Cache-Control", "public, max-age=300")
			c.Header("X-Cache", "HIT")
			c.Header("X-Layer", fmt.Sprintf("tax_parcels_%d", year))
			c.Data(http.StatusOK, "application/x-protobuf", cachedData)
			return
		}
	}

	// Try precomputed tax parcel tiles first.
	var precomputedTile []byte
	preRow := db.DB.Raw(`
		SELECT data
		FROM tiles
		WHERE z = ? AND x = ? AND y = ? AND layer = ?
	`, z, x, y, precomputedLayer).Row()
	preErr := preRow.Scan(&precomputedTile)
	if preErr == nil && len(precomputedTile) > 0 {
		if tiles.TaxParcelTilesCache != nil {
			tiles.TaxParcelTilesCache.Put(cacheKey, precomputedTile)
		}
		c.Header("Content-Type", "application/x-protobuf")
		c.Header("Content-Encoding", "gzip")
		c.Header("Cache-Control", "public, max-age=2592000, immutable")
		c.Header("X-Cache", "MISS")
		c.Header("X-Layer", fmt.Sprintf("tax_parcels_%d", year))
		c.Data(http.StatusOK, "application/x-protobuf", precomputedTile)
		return
	}

	query := `
		WITH parcel_polygons AS (
			SELECT
				ST_AsMVTGeom(
					p.geometry,
					ST_TileEnvelope($1, $2, $3),
					4096,
					16,
					true
				) AS geom,
				p.county_id || '_' || p.objectid AS feature_id,
				pt.tax_amount::double precision AS tax_amount
			FROM parcels p
			JOIN parcel_taxes pt
			  ON pt.parcel_id = p.id
			 AND pt.tax_year = $5
			WHERE p.county_id = $4
			  AND p.processed IS NULL
			  AND pt.tax_amount IS NOT NULL
			  AND pt.tax_amount > 0
			  AND p.geometry && ST_TileEnvelope($1, $2, $3)
		)
		SELECT COALESCE(
			(SELECT ST_AsMVT(parcel_polygons, 'tax_parcels', 4096, 'geom')
			 FROM parcel_polygons
			 WHERE geom IS NOT NULL),
			''::bytea
		) AS mvt
	`

	var mvtData []byte
	row := db.DB.Raw(query, z, x, y, countyID, year).Row()
	if err := row.Scan(&mvtData); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if len(mvtData) == 0 || len(mvtData) < 50 {
		c.Status(http.StatusNoContent)
		return
	}

	compressed, err := utils.Gzip(mvtData)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	if tiles.TaxParcelTilesCache != nil {
		tiles.TaxParcelTilesCache.Put(cacheKey, compressed)
	}

	c.Header("Content-Type", "application/x-protobuf")
	c.Header("Content-Encoding", "gzip")
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("X-Cache", "MISS")
	c.Header("X-Layer", fmt.Sprintf("tax_parcels_%d", year))
	c.Data(http.StatusOK, "application/x-protobuf", compressed)
}

// GetTaxHeatmapStats returns full-county value distribution stats for a year.
func GetTaxHeatmapStats(c *gin.Context) {
	year, ok := parseYearParam(c)
	if !ok {
		return
	}
	countyID := parseCountyID(c)
	cacheKey := taxHeatmapStatsCacheKey(countyID, year)

	now := time.Now()
	taxHeatmapStatsCache.mu.RLock()
	cached, exists := taxHeatmapStatsCache.data[cacheKey]
	taxHeatmapStatsCache.mu.RUnlock()
	if exists && now.Before(cached.ExpiresAt) {
		writeTaxHeatmapStatsResponse(c, cached.Data, "HIT")
		return
	}

	payload, err := loadTaxHeatmapStatsFromDB(countyID, year)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	taxHeatmapStatsCache.mu.Lock()
	taxHeatmapStatsCache.data[cacheKey] = cachedTaxHeatmapStats{
		Data:      payload,
		ExpiresAt: now.Add(taxHeatmapStatsTTL),
	}
	taxHeatmapStatsCache.mu.Unlock()

	writeTaxHeatmapStatsResponse(c, payload, "MISS")
}

// GetParcelTaxValuesBatch returns tax_amount values for a batch of parcel object IDs.
// URL format: POST /api/tax-heatmap/parcel-values  body: { "year": 2024, "county_id": 671, "object_ids": [123,456] }
func GetParcelTaxValuesBatch(c *gin.Context) {
	var req struct {
		Year      int     `json:"year"`
		CountyID  *uint16 `json:"county_id"`
		ObjectIDs []int64 `json:"object_ids"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Year < 1900 || req.Year > 2100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid year"})
		return
	}
	if len(req.ObjectIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"rows": []gin.H{}})
		return
	}
	if len(req.ObjectIDs) > 20000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many object_ids (max 20000)"})
		return
	}

	countyID := uint16(671)
	if req.CountyID != nil && *req.CountyID > 0 {
		countyID = *req.CountyID
	}

	type row struct {
		FeatureID string   `json:"feature_id"`
		TaxAmount *float64 `json:"tax_amount"`
	}
	var rows []row
	err := db.DB.Table("parcels p").
		Select("p.county_id::text || '_' || p.objectid::text AS feature_id, pt.tax_amount::double precision AS tax_amount").
		Joins("JOIN parcel_taxes pt ON pt.parcel_id = p.id AND pt.tax_year = ?", req.Year).
		Where("p.county_id = ? AND p.processed IS NULL AND p.objectid IN ?", countyID, req.ObjectIDs).
		Where("pt.tax_amount IS NOT NULL AND pt.tax_amount > 0").
		Scan(&rows).Error
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"year":      req.Year,
		"county_id": countyID,
		"rows":      rows,
	})
}
