package tiles

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/models"
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
)

// TileCoord represents a tile coordinate (Z/X/Y)
type TileCoord struct {
	Z int
	X int
	Y int
}

// TileResult holds the result of generating a single tile
type TileResult struct {
	Z       int
	X       int
	Y       int
	MVTData []byte
	Error   error
	IsEmpty bool
}

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
					geometry,  -- Geometry is already in 3857
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
			  AND geometry && ST_TileEnvelope($1, $2, $3)  -- Bbox check: both in 3857
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
	compressed, err := utils.Gzip(mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to compress MVT: %w", err)
	}

	return compressed, nil
}

// GenerateTilesForCounty generates all tiles for a county at specified zoom levels
// This is a long-running operation that pre-generates tiles
// logPerf enables performance logging with time elapsed and transactions per second
func GenerateTilesForCounty(db *gorm.DB, countyID uint16, countyName string, minZoom, maxZoom int, logPerf bool) error {
	log.Printf("Starting tile generation for %s county (zoom %d-%d)...", countyName, minZoom, maxZoom)
	log.Printf("County ID: %d", countyID)

	// Initialize performance logger
	perfLogger := utils.NewPerfLogger(logPerf)
	if logPerf {
		log.Println("Performance logging enabled")
	}

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
				  AND geometry && ST_TileEnvelope($5::int, x::int, y::int)
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
	log.Printf("Starting tile generation with 8 workers and batch size 50...")

	// Generate tiles with worker pool and batching
	storedCount, emptyCount, errorCount := generateTilesWithWorkers(db, tiles, countyID, perfLogger)

	log.Printf("Tile generation complete: %d tiles stored, %d empty, %d errors", storedCount, emptyCount, errorCount)

	// Log final performance summary
	perfLogger.LogFinal()

	if errorCount > 0 {
		return fmt.Errorf("tile generation completed with %d errors", errorCount)
	}

	return nil
}

// generateTilesWithWorkers generates tiles using a worker pool and batches insertions
func generateTilesWithWorkers(gormDB *gorm.DB, tiles []TileCoord, countyID uint16, perfLogger *utils.PerfLogger) (storedCount, emptyCount, errorCount int) {
	const numWorkers = 8
	const batchSize = 50

	totalTiles := len(tiles)
	tileChan := make(chan TileCoord, numWorkers*2)    // Buffered channel for tile coordinates
	resultChan := make(chan TileResult, numWorkers*2) // Buffered channel for results

	// WaitGroup for workers
	var wg sync.WaitGroup

	// Start worker pool (8 workers)
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for tile := range tileChan {
				// Generate MVT tile
				mvtData, err := GenerateTile(gormDB, tile.Z, tile.X, tile.Y, countyID)

				result := TileResult{
					Z:       tile.Z,
					X:       tile.X,
					Y:       tile.Y,
					MVTData: mvtData,
					Error:   err,
					IsEmpty: mvtData == nil,
				}

				resultChan <- result
			}
		}(w)
	}

	// Goroutine to close resultChan when all workers are done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Goroutine to send tiles to workers
	go func() {
		for _, tile := range tiles {
			tileChan <- tile
		}
		close(tileChan)
	}()

	// Main goroutine: collect results and batch insert
	ctx := context.Background()
	batch := &pgx.Batch{}
	batchCount := 0
	processedCount := 0

	for result := range resultChan {
		processedCount++

		if result.Error != nil {
			log.Printf("ERROR: Failed to generate tile %d/%d/%d: %v", result.Z, result.X, result.Y, result.Error)
			errorCount++
		} else if result.IsEmpty {
			emptyCount++
		} else {
			// Queue tile for batch insertion
			batch.Queue(`
				INSERT INTO tiles (z, x, y, county_id, layer, data, created_at)
				VALUES ($1, $2, $3, $4, 'parcels', $5, NOW())
				ON CONFLICT (z, x, y, county_id, layer) DO UPDATE SET
				data = EXCLUDED.data,
				created_at = NOW()
			`, result.Z, result.X, result.Y, countyID, result.MVTData)

			batchCount++

			// Flush batch when it reaches size 50 or we've processed all tiles
			if batchCount >= batchSize || processedCount == totalTiles {
				err := flushBatch(ctx, batch, batchCount)
				if err != nil {
					log.Printf("ERROR: Failed to flush batch: %v", err)
					errorCount += batchCount
				} else {
					storedCount += batchCount
				}

				// Reset batch
				batch = &pgx.Batch{}
				batchCount = 0
			}
		}

		// Update performance logger
		perfLogger.Update(processedCount, 10*time.Second)

		// Progress logging every 1000 tiles
		if processedCount%1000 == 0 || processedCount == totalTiles {
			progress := float64(processedCount) / float64(totalTiles) * 100
			log.Printf("Progress: %d/%d tiles processed (%.1f%%) - %d stored, %d empty, %d errors",
				processedCount, totalTiles, progress, storedCount, emptyCount, errorCount)
		}
	}

	// Final batch flush (in case there are remaining tiles)
	if batchCount > 0 {
		err := flushBatch(ctx, batch, batchCount)
		if err != nil {
			log.Printf("ERROR: Failed to flush final batch: %v", err)
			errorCount += batchCount
		} else {
			storedCount += batchCount
		}
	}

	return storedCount, emptyCount, errorCount
}

// flushBatch sends a pgx.Batch to the database
func flushBatch(ctx context.Context, batch *pgx.Batch, count int) error {
	if count == 0 {
		return nil
	}

	br := db.Pool.SendBatch(ctx, batch)
	defer br.Close()

	// Execute all queries in the batch
	for i := 0; i < count; i++ {
		_, err := br.Exec()
		if err != nil {
			return fmt.Errorf("batch exec failed on tile %d: %w", i, err)
		}
	}

	return nil
}
