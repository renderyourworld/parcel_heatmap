package tiles

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
)

// Represents a tile coordinate (Z/X/Y)
type tileCoord struct {
	z int
	x int
	y int
}

// Holds the result of generating a single tile
type tileResult struct {
	z       int
	x       int
	y       int
	mvtData []byte
	err     error
	isEmpty bool
}

// Sends a pgx.Batch to the database
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

// Creates an MVT (Mapbox Vector Tile) for the given Z/X/Y coordinates using PostGIS ST_AsMVT
// Returns gzipped MVT binary data
func generateTile(db *gorm.DB, z, x, y int) ([]byte, error) {
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
					p.geometry,  -- Geometry is already in 3857
					ST_TileEnvelope($1, $2, $3),   -- Tile bounds in native 3857
					4096,  -- tile extent (standard)
					$4,    -- buffer pixels (dynamic based on zoom)
					true   -- clip geometry to tile bounds
				) AS geom,
				p.county_id || '_' || p.objectid AS feature_id,  -- Composite ID: unique across all counties
				p.parcel_id,
				p.site_number,
				p.site_address,
				p.owner_name,
				p.owner_address,
				p.acres,
				p.classification,
				cc.category,
				cc.color AS class_color,
				p.tax_district
			FROM parcels p
			LEFT JOIN parcel_class_codes cc ON p.county_id = cc.county_id AND p.classification = cc.code
			WHERE p.geometry && ST_TileEnvelope($1, $2, $3)  -- Bbox check: both in 3857
			  AND p.processed IS NULL
		) AS tile
		WHERE geom IS NOT NULL
	`

	var mvtData []byte
	row := db.Raw(query, z, x, y, buffer).Row()
	err := row.Scan(&mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate MVT for tile %d/%d/%d: %w", z, x, y, err)
	}

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

// Generates tiles using a worker pool and batches insertions
func generateTilesWithWorkers(gormDB *gorm.DB, tiles []tileCoord, perfLogger *utils.PerfLogger) (storedCount, emptyCount, errorCount int) {
	const numWorkers = 8
	const batchSize = 50

	totalTiles := len(tiles)
	tileChan := make(chan tileCoord, numWorkers*2)
	resultChan := make(chan tileResult, numWorkers*2)

	// WaitGroup for workers
	var wg sync.WaitGroup

	// Start worker pool
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for tile := range tileChan {
				// Generate MVT tile
				mvtData, err := generateTile(gormDB, tile.z, tile.x, tile.y)

				result := tileResult{
					z:       tile.z,
					x:       tile.x,
					y:       tile.y,
					mvtData: mvtData,
					err:     err,
					isEmpty: mvtData == nil,
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

		if result.err != nil {
			log.Printf("ERROR: Failed to generate tile %d/%d/%d: %v", result.z, result.x, result.y, result.err)
			errorCount++
		} else if result.isEmpty {
			emptyCount++
		} else {
			// Queue tile for batch insertion
			batch.Queue(`
				INSERT INTO tiles (z, x, y, layer, data, created_at)
				VALUES ($1, $2, $3, 'parcels', $4, NOW())
				ON CONFLICT (z, x, y, layer) DO UPDATE SET
				data = EXCLUDED.data,
				created_at = NOW()
			`, result.z, result.x, result.y, result.mvtData)

			batchCount++

			// Flush batch when it reaches size the batch size or we've processed all tiles
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

// Generates all tiles for a county at specified zoom levels
func GenerateTilesForCounty(db *gorm.DB, countyID uint16, countyName string, minZoom, maxZoom int, logging bool) error {
	log.Printf("Starting tile generation for %s county (zoom %d-%d)...", countyName, minZoom, maxZoom)

	// Initialize performance logger
	perfLogger := utils.NewPerfLogger(logging)
	if logging {
		log.Println("Performance logging enabled")
	}

	// Calculate tile grid using the county's actual geometry
	var tiles []tileCoord
	for z := minZoom; z <= maxZoom; z++ {
		var zoomTiles []struct {
			X int
			Y int
		}

		// Use the county boundary geometry to find tiles
		// This handles irregular shapes (like Fulton county's 'S' shape) efficiently
		if err := db.Raw(`
			WITH county AS (
				SELECT boundary, ST_Transform(boundary, 3857) as boundary_3857, id FROM counties WHERE id = $1
			),
			tile_range AS (
				-- Calculate min/max tile indices based on county bounding box in 3857
				-- Web Mercator world is a perfect square that goes from -20,037,508.34 to +20,037,508.34 meters in both directions
				SELECT 
					FLOOR((ST_XMin(boundary_3857) + 20037508.34) / (20037508.34 * 2 / POW(2, $2::int)))::int AS min_x,
					CEIL((ST_XMax(boundary_3857) + 20037508.34) / (20037508.34 * 2 / POW(2, $2::int)))::int AS max_x,
					FLOOR((20037508.34 - ST_YMax(boundary_3857)) / (20037508.34 * 2 / POW(2, $2::int)))::int AS min_y,
					CEIL((20037508.34 - ST_YMin(boundary_3857)) / (20037508.34 * 2 / POW(2, $2::int)))::int AS max_y
				FROM county
			)
			SELECT DISTINCT x, y
			FROM tile_range,
			     generate_series(min_x, max_x) AS x,
			     generate_series(min_y, max_y) AS y
			WHERE 
				-- Must intersect the actual county polygon (not just bbox)
				ST_Intersects(ST_TileEnvelope($2::int, x::int, y::int), (SELECT boundary_3857 FROM county))
				
				-- Must verify there are actually parcels to show (avoids empty tiles in lakes/parks)
				AND EXISTS (
					SELECT 1 FROM parcels
					WHERE geometry && ST_TileEnvelope($2::int, x::int, y::int)
					  AND processed IS NULL
					LIMIT 1
				)
		`, countyID, z).Scan(&zoomTiles).Error; err != nil {
			return fmt.Errorf("failed to find tiles for zoom %d: %w", z, err)
		}

		log.Printf("Zoom %d: found %d tiles intersecting county geometry", z, len(zoomTiles))

		for _, t := range zoomTiles {
			tiles = append(tiles, tileCoord{z: z, x: t.X, y: t.Y})
		}
	}

	totalTiles := len(tiles)
	log.Printf("Generating %d tiles (zoom %d-%d)...", totalTiles, minZoom, maxZoom)
	log.Printf("Starting tile generation with 8 workers and batch size 50...")

	// Generate tiles with worker pool and batching
	storedCount, emptyCount, errorCount := generateTilesWithWorkers(db, tiles, perfLogger)

	log.Printf("Tile generation complete: %d tiles stored, %d empty, %d errors", storedCount, emptyCount, errorCount)

	// Log final performance summary
	perfLogger.LogFinal()

	if errorCount > 0 {
		return fmt.Errorf("tile generation completed with %d errors", errorCount)
	}

	return nil
}
