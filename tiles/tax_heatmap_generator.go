package tiles

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
)

type taxHeatmapResult struct {
	z       int
	x       int
	y       int
	year    int
	mvtData []byte
	err     error
	isEmpty bool
}

type taxHeatmapTask struct {
	tile tileCoord
	year int
}

type taxParcelResult struct {
	z       int
	x       int
	y       int
	year    int
	mvtData []byte
	err     error
	isEmpty bool
}

type taxParcelTask struct {
	tile tileCoord
	year int
}

func taxHeatmapLayerName(year int) string {
	return fmt.Sprintf("tax_heatmap_%d", year)
}

func taxParcelLayerName(countyID uint16, year int) string {
	return fmt.Sprintf("tax_parcels_%d_%d", countyID, year)
}

func heatmapCellSizeMeters(z int) float64 {
	// World width in Web Mercator meters.
	const worldWidth = 40075016.68557849
	tileWidth := worldWidth / math.Pow(2, float64(z))

	// Increase to 64x64 bins per tile for even higher visual detail.
	return tileWidth / 64.0
}

func generateTaxHeatmapTile(gormDB *gorm.DB, z, x, y int, countyID uint16, year int) ([]byte, error) {
	cellSize := heatmapCellSizeMeters(z)

	query := `
		WITH tile AS (
			SELECT ST_TileEnvelope($1, $2, $3) AS env
		),
		points AS (
			SELECT
				ST_PointOnSurface(p.geometry) AS pt,
				ptax.tax_amount::double precision AS tax_amount
			FROM parcels p
			JOIN parcel_taxes ptax
			  ON ptax.parcel_id = p.id
			 AND ptax.tax_year = $5
			WHERE p.county_id = $4
			  AND p.processed IS NULL
			  AND ptax.tax_amount IS NOT NULL
			  AND ptax.tax_amount > 0
			  AND p.geometry && (SELECT env FROM tile)
		),
		bins AS (
			SELECT
				FLOOR(ST_X(pt) / $6) * $6 AS gx,
				FLOOR(ST_Y(pt) / $6) * $6 AS gy,
				AVG(tax_amount) AS avg_tax_amount,
				COUNT(*)::int AS parcel_count
			FROM points
			GROUP BY 1, 2
		),
		cells AS (
			SELECT
				ST_AsMVTGeom(
					ST_MakeEnvelope(gx, gy, gx + $6, gy + $6, 3857),
					(SELECT env FROM tile),
					4096,
					0,
					true
				) AS geom,
				avg_tax_amount,
				parcel_count
			FROM bins
		)
		SELECT COALESCE(
			(SELECT ST_AsMVT(cells, 'tax_heatmap', 4096, 'geom')
			 FROM cells
			 WHERE geom IS NOT NULL),
			''::bytea
		) AS mvt
	`

	var mvtData []byte
	row := gormDB.Raw(query, z, x, y, countyID, year, cellSize).Row()
	if err := row.Scan(&mvtData); err != nil {
		return nil, fmt.Errorf("failed generating heatmap tile %d/%d/%d year=%d: %w", z, x, y, year, err)
	}

	if len(mvtData) == 0 || len(mvtData) < 50 {
		return nil, nil
	}

	compressed, err := utils.Gzip(mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to gzip heatmap tile %d/%d/%d year=%d: %w", z, x, y, year, err)
	}
	return compressed, nil
}

func generateTaxHeatmapTiles(gormDB *gorm.DB, countyID uint16, years []int, minZoom, maxZoom int) ([]tileCoord, error) {
	var tiles []tileCoord
	for z := minZoom; z <= maxZoom; z++ {
		var zoomTiles []struct {
			X int
			Y int
		}

		err := gormDB.Raw(`
			WITH county AS (
				SELECT ST_Transform(boundary, 3857) AS boundary_3857
				FROM counties
				WHERE id = $1
			),
			tile_range AS (
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
			WHERE ST_Intersects(ST_TileEnvelope($2::int, x::int, y::int), (SELECT boundary_3857 FROM county))
			  AND EXISTS (
				SELECT 1
				FROM parcels p
				JOIN parcel_taxes pt
				  ON pt.parcel_id = p.id
				 AND pt.tax_year = ANY($3)
				WHERE p.county_id = $1
				  AND p.processed IS NULL
				  AND pt.tax_amount IS NOT NULL
				  AND pt.tax_amount > 0
				  AND p.geometry && ST_TileEnvelope($2::int, x::int, y::int)
				LIMIT 1
			  )
		`, countyID, z, years).Scan(&zoomTiles).Error
		if err != nil {
			return nil, fmt.Errorf("failed finding tax heatmap tiles at zoom %d: %w", z, err)
		}

		log.Printf("Tax heatmap zoom %d: found %d tiles", z, len(zoomTiles))
		for _, t := range zoomTiles {
			tiles = append(tiles, tileCoord{z: z, x: t.X, y: t.Y})
		}
	}

	return tiles, nil
}

func generateTaxHeatmapWithWorkers(gormDB *gorm.DB, countyID uint16, years []int, tilesToBuild []tileCoord, perfLogger *utils.PerfLogger) (stored, empty, errorsCount int) {
	const numWorkers = 24
	const batchSize = 100

	total := len(tilesToBuild) * len(years)
	taskChan := make(chan taxHeatmapTask, numWorkers*2)
	resultChan := make(chan taxHeatmapResult, numWorkers*2)

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				mvtData, err := generateTaxHeatmapTile(
					gormDB,
					task.tile.z,
					task.tile.x,
					task.tile.y,
					countyID,
					task.year,
				)
				resultChan <- taxHeatmapResult{
					z:       task.tile.z,
					x:       task.tile.x,
					y:       task.tile.y,
					year:    task.year,
					mvtData: mvtData,
					err:     err,
					isEmpty: mvtData == nil,
				}
			}
		}()
	}

	go func() {
		for _, year := range years {
			log.Printf("Queueing generation tasks for year=%d layer=%s", year, taxHeatmapLayerName(year))
			for _, t := range tilesToBuild {
				taskChan <- taxHeatmapTask{
					tile: t,
					year: year,
				}
			}
		}
		close(taskChan)
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	ctx := context.Background()
	batch := &pgx.Batch{}
	batchCount := 0
	processed := 0

	for result := range resultChan {
		processed++

		if result.err != nil {
			log.Printf("ERROR: heatmap tile generation failed year=%d tile=%d/%d/%d: %v", result.year, result.z, result.x, result.y, result.err)
			errorsCount++
		} else if result.isEmpty {
			empty++
		} else {
			batch.Queue(`
				INSERT INTO tiles (z, x, y, layer, data, created_at)
				VALUES ($1, $2, $3, $4, $5, NOW())
				ON CONFLICT (z, x, y, layer) DO UPDATE SET
				  data = EXCLUDED.data,
				  created_at = NOW()
			`, result.z, result.x, result.y, taxHeatmapLayerName(result.year), result.mvtData)
			batchCount++
		}

		if batchCount >= batchSize {
			if err := flushBatch(ctx, batch, batchCount); err != nil {
				log.Printf("ERROR: tax heatmap batch flush failed: %v", err)
				errorsCount += batchCount
			} else {
				stored += batchCount
			}
			batch = &pgx.Batch{}
			batchCount = 0
		}

		perfLogger.Update(processed, 10*time.Second)
		if processed%1000 == 0 || processed == total {
			progress := float64(processed) / float64(total) * 100
			log.Printf("Tax heatmap progress: %d/%d (%.1f%%) stored=%d empty=%d errors=%d",
				processed, total, progress, stored, empty, errorsCount)
		}
	}

	if batchCount > 0 {
		if err := flushBatch(ctx, batch, batchCount); err != nil {
			log.Printf("ERROR: final tax heatmap batch flush failed: %v", err)
			errorsCount += batchCount
		} else {
			stored += batchCount
		}
	}

	return stored, empty, errorsCount
}

func generateTaxParcelTile(gormDB *gorm.DB, z, x, y int, countyID uint16, year int) ([]byte, error) {
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
	row := gormDB.Raw(query, z, x, y, countyID, year).Row()
	if err := row.Scan(&mvtData); err != nil {
		return nil, fmt.Errorf("failed generating tax parcel tile %d/%d/%d year=%d: %w", z, x, y, year, err)
	}
	if len(mvtData) == 0 || len(mvtData) < 50 {
		return nil, nil
	}

	compressed, err := utils.Gzip(mvtData)
	if err != nil {
		return nil, fmt.Errorf("failed to gzip tax parcel tile %d/%d/%d year=%d: %w", z, x, y, year, err)
	}
	return compressed, nil
}

func generateTaxParcelTiles(gormDB *gorm.DB, countyID uint16, years []int, minZoom, maxZoom int) ([]tileCoord, error) {
	var tiles []tileCoord
	for z := minZoom; z <= maxZoom; z++ {
		var zoomTiles []struct {
			X int
			Y int
		}

		err := gormDB.Raw(`
			WITH qualified_parcels AS (
				SELECT p.geometry
				FROM parcels p
				JOIN parcel_taxes pt
				  ON pt.parcel_id = p.id
				 AND pt.tax_year = ANY($3)
				WHERE p.county_id = $1
				  AND p.processed IS NULL
				  AND pt.tax_amount IS NOT NULL
				  AND pt.tax_amount > 0
			),
			parcel_tile_ranges AS (
				SELECT
					FLOOR((ST_XMin(geometry) + 20037508.34) / (40075016.68 / POW(2, $2::int)))::int AS min_x,
					FLOOR((ST_XMax(geometry) + 20037508.34) / (40075016.68 / POW(2, $2::int)))::int AS max_x,
					FLOOR((20037508.34 - ST_YMax(geometry)) / (40075016.68 / POW(2, $2::int)))::int AS min_y,
					FLOOR((20037508.34 - ST_YMin(geometry)) / (40075016.68 / POW(2, $2::int)))::int AS max_y
				FROM qualified_parcels
			)
			SELECT DISTINCT x, y
			FROM parcel_tile_ranges,
			     generate_series(min_x, max_x) AS x,
			     generate_series(min_y, max_y) AS y
			WHERE x >= 0
			  AND y >= 0
			  AND x < POW(2, $2::int)
			  AND y < POW(2, $2::int)
		`, countyID, z, years).Scan(&zoomTiles).Error
		if err != nil {
			return nil, fmt.Errorf("failed finding tax parcel tiles at zoom %d: %w", z, err)
		}

		log.Printf("Tax parcel zoom %d: found %d tiles", z, len(zoomTiles))
		for _, t := range zoomTiles {
			tiles = append(tiles, tileCoord{z: z, x: t.X, y: t.Y})
		}
	}

	return tiles, nil
}

func generateTaxParcelsWithWorkers(gormDB *gorm.DB, countyID uint16, years []int, tilesToBuild []tileCoord, perfLogger *utils.PerfLogger) (stored, empty, errorsCount int) {
	const numWorkers = 24
	const batchSize = 100

	total := len(tilesToBuild) * len(years)
	taskChan := make(chan taxParcelTask, numWorkers*2)
	resultChan := make(chan taxParcelResult, numWorkers*2)

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				mvtData, err := generateTaxParcelTile(
					gormDB,
					task.tile.z,
					task.tile.x,
					task.tile.y,
					countyID,
					task.year,
				)
				resultChan <- taxParcelResult{
					z:       task.tile.z,
					x:       task.tile.x,
					y:       task.tile.y,
					year:    task.year,
					mvtData: mvtData,
					err:     err,
					isEmpty: mvtData == nil,
				}
			}
		}()
	}

	go func() {
		for _, year := range years {
			log.Printf("Queueing tax parcel generation tasks for year=%d layer=%s", year, taxParcelLayerName(countyID, year))
			for _, t := range tilesToBuild {
				taskChan <- taxParcelTask{
					tile: t,
					year: year,
				}
			}
		}
		close(taskChan)
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	ctx := context.Background()
	batch := &pgx.Batch{}
	batchCount := 0
	processed := 0

	for result := range resultChan {
		processed++

		if result.err != nil {
			log.Printf("ERROR: tax parcel tile generation failed year=%d tile=%d/%d/%d: %v", result.year, result.z, result.x, result.y, result.err)
			errorsCount++
		} else if result.isEmpty {
			empty++
		} else {
			batch.Queue(`
				INSERT INTO tiles (z, x, y, layer, data, created_at)
				VALUES ($1, $2, $3, $4, $5, NOW())
				ON CONFLICT (z, x, y, layer) DO UPDATE SET
				  data = EXCLUDED.data,
				  created_at = NOW()
			`, result.z, result.x, result.y, taxParcelLayerName(countyID, result.year), result.mvtData)
			batchCount++
		}

		if batchCount >= batchSize {
			if err := flushBatch(ctx, batch, batchCount); err != nil {
				log.Printf("ERROR: tax parcel batch flush failed: %v", err)
				errorsCount += batchCount
			} else {
				stored += batchCount
			}
			batch = &pgx.Batch{}
			batchCount = 0
		}

		perfLogger.Update(processed, 10*time.Second)
		if processed%1000 == 0 || processed == total {
			progress := float64(processed) / float64(total) * 100
			log.Printf("Tax parcel progress: %d/%d (%.1f%%) stored=%d empty=%d errors=%d",
				processed, total, progress, stored, empty, errorsCount)
		}
	}

	if batchCount > 0 {
		if err := flushBatch(ctx, batch, batchCount); err != nil {
			log.Printf("ERROR: final tax parcel batch flush failed: %v", err)
			errorsCount += batchCount
		} else {
			stored += batchCount
		}
	}

	return stored, empty, errorsCount
}

// GenerateTaxHeatmapTilesForCounty precomputes heatmap tiles for tax amount by year.
func GenerateTaxHeatmapTilesForCounty(gormDB *gorm.DB, countyID uint16, countyName string, years []int, minZoom, maxZoom int, logging bool) error {
	log.Printf("Starting tax heatmap generation for %s county years=%v zoom=%d-%d", countyName, years, minZoom, maxZoom)

	if len(years) == 0 {
		return fmt.Errorf("no tax years provided")
	}

	perfLogger := utils.NewPerfLogger(logging)

	tilesToBuild, err := generateTaxHeatmapTiles(gormDB, countyID, years, minZoom, maxZoom)
	if err != nil {
		return err
	}
	if len(tilesToBuild) == 0 {
		log.Printf("No tax heatmap tiles found for county=%d years=%v", countyID, years)
		return nil
	}

	total := len(tilesToBuild) * len(years)
	log.Printf("Generating %d tax heatmap tiles total (%d tile coords x %d years)", total, len(tilesToBuild), len(years))
	stored, empty, errorsCount := generateTaxHeatmapWithWorkers(gormDB, countyID, years, tilesToBuild, perfLogger)

	log.Printf("Tax heatmap generation complete: stored=%d empty=%d errors=%d", stored, empty, errorsCount)
	perfLogger.LogFinal()

	if errorsCount > 0 {
		return fmt.Errorf("tax heatmap generation completed with %d errors", errorsCount)
	}

	return nil
}

// GenerateTaxParcelTilesForCounty precomputes parcel-level tax tiles by year (layer: tax_parcels_<county>_<year>).
func GenerateTaxParcelTilesForCounty(gormDB *gorm.DB, countyID uint16, countyName string, years []int, minZoom, maxZoom int, logging bool) error {
	log.Printf("Starting tax parcel tile generation for %s county years=%v zoom=%d-%d", countyName, years, minZoom, maxZoom)

	if len(years) == 0 {
		return fmt.Errorf("no tax years provided")
	}

	perfLogger := utils.NewPerfLogger(logging)

	tilesToBuild, err := generateTaxParcelTiles(gormDB, countyID, years, minZoom, maxZoom)
	if err != nil {
		return err
	}
	if len(tilesToBuild) == 0 {
		log.Printf("No tax parcel tiles found for county=%d years=%v", countyID, years)
		return nil
	}

	total := len(tilesToBuild) * len(years)
	log.Printf("Generating %d tax parcel tiles total (%d tile coords x %d years)", total, len(tilesToBuild), len(years))
	stored, empty, errorsCount := generateTaxParcelsWithWorkers(gormDB, countyID, years, tilesToBuild, perfLogger)

	log.Printf("Tax parcel tile generation complete: stored=%d empty=%d errors=%d", stored, empty, errorsCount)
	perfLogger.LogFinal()

	if errorsCount > 0 {
		return fmt.Errorf("tax parcel tile generation completed with %d errors", errorsCount)
	}

	return nil
}
