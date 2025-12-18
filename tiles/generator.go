package tiles

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log"

	"github.com/renderyourworld/parcel_heatmap/models"
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
)

// GenerateTile creates an MVT tile for the given Z/X/Y coordinates using PostGIS ST_AsMVT
// Returns gzipped MVT binary data ready for storage or serving
func GenerateTile(db *gorm.DB, z, x, y int, countyID uint16) ([]byte, error) {
	// MVT tiles are in EPSG:3857 (Web Mercator) coordinate space
	// ST_AsMVTGeom expects geometry and tile bounds both in 3857

	// Use progressively smaller buffer at higher zoom levels to reduce duplicate labels
	// At higher zooms, parcels are larger on screen and need less buffer
	buffer := 256
	if z >= 17 {
		buffer = 32
	}
	if z >= 18 {
		buffer = 8
	}
	if z >= 19 {
		buffer = 0
	}

	query := `
		SELECT ST_AsMVT(tile, 'parcels', 4096, 'geom') AS mvt
		FROM (
			SELECT 
				ST_AsMVTGeom(
					ST_Transform(geometry, 3857),  -- Transform parcel from 4326 to 3857
					ST_TileEnvelope($1, $2, $3),   -- Tile bounds in native 3857
					4096,  -- tile extent (standard)
					$5,    -- buffer pixels (dynamic based on zoom)
					true   -- clip geometry to tile bounds
				) AS geom,
				county_id || '_' || objectid AS feature_id,  -- Composite ID: unique across all counties
				objectid,
				parcel_id,
				site_number,
				site_address,
				owner_name,
				owner_address,
				acres,
				classification,
				tax_district
			FROM parcels
			WHERE county_id = $4
			  AND geometry && ST_Transform(ST_TileEnvelope($1, $2, $3), 4326)  -- Bbox check: transform tile to 4326
			  AND processed IS NULL
		) AS tile
		WHERE geom IS NOT NULL
	`

	var mvtData []byte
	row := db.Raw(query, z, x, y, countyID, buffer).Row()
	err := row.Scan(&mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate MVT for tile %d/%d/%d: %w", z, x, y, err)
	}

	// Empty tile (no parcels in this area)
	// ST_AsMVT returns a small empty MVT blob even when there are no features
	// Reject tiles smaller than ~50 bytes as they're likely empty
	if len(mvtData) == 0 || len(mvtData) < 50 {
		return nil, nil
	}

	// Gzip compress for storage
	compressed, err := gzipCompress(mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to compress MVT: %w", err)
	}

	return compressed, nil
}

// GenerateTilesForCounty generates all tiles for a county at specified zoom levels
// This is a long-running operation that pre-generates tiles
func GenerateTilesForCounty(db *gorm.DB, countyID uint16, countyName string, minZoom, maxZoom int) error {
	log.Printf("Starting tile generation for %s county (zoom %d-%d)...", countyName, minZoom, maxZoom)
	log.Printf("County ID: %d", countyID)

	// Get county bounds from database
	var county models.County
	if err := db.Select("id, name").Where("id = ?", countyID).First(&county).Error; err != nil {
		return fmt.Errorf("county not found: %w", err)
	}

	// Check how many parcels exist for this county
	var parcelCount int64
	db.Model(&models.Parcel{}).Where("county_id = ?", countyID).Count(&parcelCount)
	log.Printf("Found %d parcels for county_id=%d", parcelCount, countyID)

	// Check how many successful parcels exist
	var successCount int64
	db.Model(&models.Parcel{}).Where("county_id = ? AND processed IS NULL", countyID).Count(&successCount)
	log.Printf("Found %d successfully processed parcels", successCount)

	// Query county bounding box
	var bounds utils.BBox
	err := db.Raw(`
		SELECT 
			ST_XMin(boundary) as min_lon,
			ST_YMin(boundary) as min_lat,
			ST_XMax(boundary) as max_lon,
			ST_YMax(boundary) as max_lat
		FROM counties WHERE id = ?
	`, countyID).Scan(&bounds).Error

	if err != nil {
		return fmt.Errorf("failed to get county bounds: %w", err)
	}

	log.Printf("County bounds: [%.4f, %.4f, %.4f, %.4f]", bounds.MinLon, bounds.MinLat, bounds.MaxLon, bounds.MaxLat)

	// Calculate tile grid dynamically from county bounds
	// Use PostGIS to find which tiles actually contain parcels
	type TileCoord struct {
		Z int
		X int
		Y int
	}

	var tiles []TileCoord
	for z := minZoom; z <= maxZoom; z++ {
		// Calculate tile range from county bounds using Web Mercator math
		// PostGIS provides tile coordinate functions based on 3857 coordinates
		var zoomTiles []struct {
			X int
			Y int
		}

		// Query PostGIS to find unique tiles that contain parcels
		// Uses ST_TileEnvelope in its native 3857, transforms to 4326 for bbox check
		err = db.Raw(`
			WITH bbox AS (
				SELECT ST_Transform(ST_MakeEnvelope($1, $2, $3, $4, 4326), 3857) AS geom
			),
			tile_range AS (
				SELECT 
					FLOOR((ST_XMin((SELECT geom FROM bbox)) + 20037508.34) / (20037508.34 * 2 / POW(2, $5::int)))::int AS min_x,
					CEIL((ST_XMax((SELECT geom FROM bbox)) + 20037508.34) / (20037508.34 * 2 / POW(2, $5::int)))::int AS max_x,
					FLOOR((20037508.34 - ST_YMax((SELECT geom FROM bbox))) / (20037508.34 * 2 / POW(2, $5::int)))::int AS min_y,
					CEIL((20037508.34 - ST_YMin((SELECT geom FROM bbox))) / (20037508.34 * 2 / POW(2, $5::int)))::int AS max_y
			)
			SELECT DISTINCT x, y
			FROM tile_range,
			     generate_series(min_x, max_x) AS x,
			     generate_series(min_y, max_y) AS y
			WHERE EXISTS (
				SELECT 1 FROM parcels
				WHERE county_id = $6
				  AND processed IS NULL
				  AND geometry && ST_Transform(ST_TileEnvelope($5::int, x::int, y::int), 4326)
				LIMIT 1
			)
		`, bounds.MinLon, bounds.MinLat, bounds.MaxLon, bounds.MaxLat, z, countyID).Scan(&zoomTiles).Error

		if err != nil {
			return fmt.Errorf("failed to find tiles for zoom %d: %w", z, err)
		}

		log.Printf("Zoom %d: found %d tiles with parcels", z, len(zoomTiles))

		for _, t := range zoomTiles {
			tiles = append(tiles, TileCoord{Z: z, X: t.X, Y: t.Y})
		}
	}

	totalTiles := len(tiles)
	log.Printf("Generating %d tiles (zoom %d-%d)...", totalTiles, minZoom, maxZoom)
	log.Printf("Starting tile generation loop...")

	// Generate tiles with progress tracking
	storedCount := 0
	emptyCount := 0
	errorCount := 0

	for i, tile := range tiles {
		// Generate MVT tile
		mvtData, err := GenerateTile(db, tile.Z, tile.X, tile.Y, countyID)

		if err != nil {
			log.Printf("ERROR: Failed to generate tile %d/%d/%d: %v", tile.Z, tile.X, tile.Y, err)
			errorCount++
			continue
		}

		// Skip empty tiles (no parcels in this area)
		if mvtData == nil {
			emptyCount++
		} else {
			// Insert tile into database using raw SQL for ON CONFLICT support
			err = db.Exec(`
				INSERT INTO tiles (z, x, y, county_id, layer, data, created_at)
				VALUES ($1, $2, $3, $4, 'parcels', $5, NOW())
				ON CONFLICT (z, x, y, county_id, layer) DO UPDATE SET
				data = EXCLUDED.data,
				created_at = NOW()
			`, tile.Z, tile.X, tile.Y, countyID, mvtData).Error

			if err != nil {
				log.Printf("ERROR: Failed to insert tile %d/%d/%d: %v", tile.Z, tile.X, tile.Y, err)
				errorCount++
			} else {
				storedCount++
			}
		}

		// Progress logging every 1000 tiles to reduce noise
		if (i+1)%1000 == 0 || i+1 == totalTiles {
			progress := float64(i+1) / float64(totalTiles) * 100
			log.Printf("Progress: %d/%d tiles processed (%.1f%%) - %d stored, %d empty, %d errors",
				i+1, totalTiles, progress, storedCount, emptyCount, errorCount)
		}
	}

	log.Printf("Tile generation complete: %d tiles stored, %d empty, %d errors", storedCount, emptyCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("tile generation completed with %d errors", errorCount)
	}

	return nil
}

// gzipCompress compresses data using gzip compression
func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	if _, err := gz.Write(data); err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
