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
	// At higher zooms, parcels are larger on screen and need less buffer.
	// Keep a non-zero floor to avoid visible tile-edge seams from hard clipping.
	buffer := 256
	if z >= 17 {
		buffer = 32
	}
	if z >= 18 {
		buffer = 16
	}

	query := `
		WITH parcel_polygons AS (
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
		),
		parcel_label_points AS (
			SELECT
				ST_AsMVTGeom(
					label_geom_3857,
					ST_TileEnvelope($1, $2, $3),
					4096,
					0,
					true
				) AS geom,
				feature_id,
				site_number
			FROM (
				SELECT
					p.county_id || '_' || p.objectid AS feature_id,
					p.site_number,
					ST_PointOnSurface(p.geometry) AS label_geom_3857
				FROM parcels p
				WHERE p.processed IS NULL
				  AND p.site_number IS NOT NULL
				  AND p.site_number <> ''
				  AND p.geometry && ST_TileEnvelope($1, $2, $3)
			) labels
			WHERE FLOOR((ST_X(label_geom_3857) + 20037508.34) / (40075016.68 / POW(2, $1::int)))::int = $2
			  AND FLOOR((20037508.34 - ST_Y(label_geom_3857)) / (40075016.68 / POW(2, $1::int)))::int = $3
		)
		SELECT
			COALESCE(
				(SELECT ST_AsMVT(parcel_polygons, 'parcels', 4096, 'geom')
				 FROM parcel_polygons
				 WHERE geom IS NOT NULL),
				''::bytea
			) ||
			COALESCE(
				(SELECT ST_AsMVT(parcel_label_points, 'parcel_labels', 4096, 'geom')
				 FROM parcel_label_points
				 WHERE geom IS NOT NULL),
				''::bytea
			) AS mvt
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
	const numWorkers = 24
	const batchSize = 100

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

// generateCountyTile creates an MVT tile for county boundaries at the given Z/X/Y
// Returns gzipped MVT binary data
func generateCountyTile(db *gorm.DB, z, x, y int) ([]byte, error) {
	query := `
		WITH county_polygons AS (
			SELECT 
				ST_AsMVTGeom(
					ST_Transform(c.boundary, 3857),
					ST_TileEnvelope($1, $2, $3),
					4096,
					256,
					true
				) AS geom,
				c.id,
				c.name,
				c.state,
				c.population,
				c.region,
				c.acres,
				c.square_miles
			FROM counties c
			WHERE ST_Transform(c.boundary, 3857) && ST_TileEnvelope($1, $2, $3)
		),
		county_label_points AS (
			SELECT
				ST_AsMVTGeom(
					label_geom_3857,
					ST_TileEnvelope($1, $2, $3),
					4096,
					0,
					true
				) AS geom,
				id,
				name
			FROM (
				SELECT
					c.id,
					c.name,
					ST_Transform(COALESCE(c.centroid, ST_PointOnSurface(c.boundary)), 3857) AS label_geom_3857
				FROM counties c
			) labels
			WHERE FLOOR((ST_X(label_geom_3857) + 20037508.34) / (40075016.68 / POW(2, $1::int)))::int = $2
			  AND FLOOR((20037508.34 - ST_Y(label_geom_3857)) / (40075016.68 / POW(2, $1::int)))::int = $3
		)
		SELECT
			COALESCE(
				(SELECT ST_AsMVT(county_polygons, 'counties', 4096, 'geom')
				 FROM county_polygons
				 WHERE geom IS NOT NULL),
				''::bytea
			) ||
			COALESCE(
				(SELECT ST_AsMVT(county_label_points, 'county_labels', 4096, 'geom')
				 FROM county_label_points
				 WHERE geom IS NOT NULL),
				''::bytea
			) AS mvt
	`

	var mvtData []byte
	row := db.Raw(query, z, x, y).Row()
	err := row.Scan(&mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate county MVT for tile %d/%d/%d: %w", z, x, y, err)
	}

	// ST_AsMVT returns a small empty MVT blob even when there are no features
	if len(mvtData) == 0 || len(mvtData) < 50 {
		return nil, nil
	}

	// Gzip compress for storage
	compressed, err := utils.Gzip(mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to compress county MVT: %w", err)
	}

	return compressed, nil
}

// GenerateCountyTiles generates vector tiles for all Georgia county boundaries
func GenerateCountyTiles(db *gorm.DB, minZoom, maxZoom int, logging bool) error {
	log.Printf("Starting county tile generation (zoom %d-%d)...", minZoom, maxZoom)

	// Initialize performance logger
	perfLogger := utils.NewPerfLogger(logging)

	// Get Georgia bounding box from all counties
	var bounds struct {
		MinX float64
		MinY float64
		MaxX float64
		MaxY float64
	}

	if err := db.Raw(`
		SELECT 
			ST_XMin(ST_Transform(ST_Collect(boundary), 3857)) as min_x,
			ST_YMin(ST_Transform(ST_Collect(boundary), 3857)) as min_y,
			ST_XMax(ST_Transform(ST_Collect(boundary), 3857)) as max_x,
			ST_YMax(ST_Transform(ST_Collect(boundary), 3857)) as max_y
		FROM counties
	`).Scan(&bounds).Error; err != nil {
		return fmt.Errorf("failed to get Georgia bounds: %w", err)
	}

	// Calculate tile coordinates for each zoom level
	var tiles []tileCoord
	for z := minZoom; z <= maxZoom; z++ {
		var zoomTiles []struct {
			X int
			Y int
		}

		// Calculate tile range for Georgia at this zoom level
		if err := db.Raw(`
			WITH bounds AS (
				SELECT ST_Transform(ST_Collect(boundary), 3857) AS geom FROM counties
			),
			tile_range AS (
				SELECT 
					FLOOR((ST_XMin(geom) + 20037508.34) / (20037508.34 * 2 / POW(2, $1::int)))::int AS min_x,
					CEIL((ST_XMax(geom) + 20037508.34) / (20037508.34 * 2 / POW(2, $1::int)))::int AS max_x,
					FLOOR((20037508.34 - ST_YMax(geom)) / (20037508.34 * 2 / POW(2, $1::int)))::int AS min_y,
					CEIL((20037508.34 - ST_YMin(geom)) / (20037508.34 * 2 / POW(2, $1::int)))::int AS max_y
				FROM bounds
			)
			SELECT DISTINCT x, y
			FROM tile_range,
			     generate_series(min_x, max_x) AS x,
			     generate_series(min_y, max_y) AS y
			WHERE ST_Intersects(
				ST_TileEnvelope($1::int, x::int, y::int), 
				(SELECT geom FROM bounds)
			)
		`, z).Scan(&zoomTiles).Error; err != nil {
			return fmt.Errorf("failed to find county tiles for zoom %d: %w", z, err)
		}

		log.Printf("Zoom %d: found %d tiles covering Georgia", z, len(zoomTiles))

		for _, t := range zoomTiles {
			tiles = append(tiles, tileCoord{z: z, x: t.X, y: t.Y})
		}
	}

	totalTiles := len(tiles)
	log.Printf("Generating %d county tiles (zoom %d-%d)...", totalTiles, minZoom, maxZoom)

	// Generate tiles (simpler approach since there are fewer tiles than parcels)
	ctx := context.Background()
	batch := &pgx.Batch{}
	batchCount := 0
	storedCount := 0
	emptyCount := 0
	errorCount := 0

	for i, tile := range tiles {
		mvtData, err := generateCountyTile(db, tile.z, tile.x, tile.y)
		if err != nil {
			log.Printf("ERROR: Failed to generate county tile %d/%d/%d: %v", tile.z, tile.x, tile.y, err)
			errorCount++
			continue
		}

		if mvtData == nil {
			emptyCount++
			continue
		}

		// Queue tile for batch insertion
		batch.Queue(`
			INSERT INTO tiles (z, x, y, layer, data, created_at)
			VALUES ($1, $2, $3, 'counties', $4, NOW())
			ON CONFLICT (z, x, y, layer) DO UPDATE SET
			data = EXCLUDED.data,
			created_at = NOW()
		`, tile.z, tile.x, tile.y, mvtData)
		batchCount++

		// Flush batch
		if batchCount >= 50 || i == totalTiles-1 {
			if err := flushBatch(ctx, batch, batchCount); err != nil {
				log.Printf("ERROR: Failed to flush batch: %v", err)
				errorCount += batchCount
			} else {
				storedCount += batchCount
			}
			batch = &pgx.Batch{}
			batchCount = 0
		}

		// Progress logging
		perfLogger.Update(i+1, 10*time.Second)
		if (i+1)%100 == 0 || i == totalTiles-1 {
			progress := float64(i+1) / float64(totalTiles) * 100
			log.Printf("Progress: %d/%d tiles (%.1f%%) - %d stored, %d empty, %d errors",
				i+1, totalTiles, progress, storedCount, emptyCount, errorCount)
		}
	}

	log.Printf("County tile generation complete: %d tiles stored, %d empty, %d errors", storedCount, emptyCount, errorCount)
	perfLogger.LogFinal()

	if errorCount > 0 {
		return fmt.Errorf("county tile generation completed with %d errors", errorCount)
	}

	return nil
}
