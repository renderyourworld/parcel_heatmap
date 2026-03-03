package importers

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

const parcelSearchBatchSize = 20000

type parcelSearchBatchResult struct {
	MaxID      int64 `gorm:"column:max_id"`
	BatchCount int   `gorm:"column:batch_count"`
}

// StartParcelSearchBuilder builds/refreshes parcel_search rows from parcels in batches.
// City/ZIP values are derived locally via ZIP polygon and ZIP->city lookup tables.
func StartParcelSearchBuilder(gormDB *gorm.DB, countyName string, resume bool, maxParcels int) error {
	if os.Getenv("RESUME") == "true" {
		resume = true
	}

	if err := EnsureZipReferenceData(gormDB); err != nil {
		return fmt.Errorf("failed preparing ZIP reference data: %w", err)
	}

	countyFilterID := 0
	checkpointCountyName := countyName
	if checkpointCountyName == "" || strings.EqualFold(checkpointCountyName, "all") {
		checkpointCountyName = "all"
	} else {
		var county models.County
		if err := gormDB.Where("name = ?", countyName).First(&county).Error; err != nil {
			return fmt.Errorf("county '%s' not found: %w", countyName, err)
		}
		countyFilterID = int(county.ID)
	}

	checkpoint, err := initializeCheckpoint(gormDB, checkpointCountyName, "parcel_search", resume)
	if err != nil {
		return fmt.Errorf("failed to initialize parcel_search checkpoint: %w", err)
	}

	currentID := checkpoint.LastProcessedID
	totalProcessed := checkpoint.TotalProcessed
	totalFailed := checkpoint.TotalFailed

	log.Printf(
		"Starting parcel_search build for county=%s (resume=%v, last_processed_id=%d, max=%d)",
		checkpointCountyName, resume, currentID, maxParcels,
	)

	for {
		batchLimit := parcelSearchBatchSize
		if maxParcels > 0 {
			remaining := maxParcels - totalProcessed
			if remaining <= 0 {
				log.Printf("Reached max parcel_search build limit (%d).", maxParcels)
				break
			}
			if remaining < batchLimit {
				batchLimit = remaining
			}
		}

		var result parcelSearchBatchResult
		if err := gormDB.Raw(`
			WITH candidate AS (
				SELECT
					p.id AS parcel_pk_id,
					p.county_id,
					p.objectid,
					co.name AS county_name,
					p.site_address,
					regexp_replace(trim(coalesce(p.site_address, '')), '\s+', ' ', 'g') AS site_address_clean,
					lower(regexp_replace(trim(coalesce(p.site_address, '')), '\s+', ' ', 'g')) AS site_address_norm,
					p.search_lat AS lat,
					p.search_lng AS lng,
					z.zip5 AS mailing_zip5,
					city_lookup.city AS mailing_city
				FROM parcels p
				JOIN counties co
				  ON co.id = p.county_id
				LEFT JOIN us_zip5_areas z
				  ON z.geom && ST_SetSRID(ST_MakePoint(p.search_lng, p.search_lat), 4326)
				 AND ST_Covers(z.geom, ST_SetSRID(ST_MakePoint(p.search_lng, p.search_lat), 4326))
				LEFT JOIN LATERAL (
					SELECT l.city
					FROM us_zip5_city_lookup l
					WHERE l.zip5 = z.zip5
					  AND l.state = 'GA'
					ORDER BY l.is_preferred DESC, l.city ASC
					LIMIT 1
				) AS city_lookup ON TRUE
				WHERE co.state = 'GA'
				  AND p.processed IS NULL
				  AND p.objectid IS NOT NULL
				  AND p.search_lat IS NOT NULL
				  AND p.search_lng IS NOT NULL
				  AND p.id > ?
				  AND (? = 0 OR p.county_id = ?)
				ORDER BY p.id
				LIMIT ?
			),
			upserted AS (
				INSERT INTO parcel_search (
					parcel_id,
					county_id,
					objectid,
					site_address,
					site_address_norm,
					mailing_city,
					mailing_zip5,
					mailing_zip4,
					display_address,
					lat,
					lng,
					updated_at
				)
				SELECT
					c.parcel_pk_id,
					c.county_id,
					c.objectid,
					c.site_address,
					NULLIF(c.site_address_norm, ''),
					c.mailing_city,
					c.mailing_zip5,
					NULL,
					CASE
						WHEN NULLIF(c.site_address_clean, '') IS NULL THEN NULL
						WHEN c.mailing_city IS NOT NULL AND c.mailing_zip5 IS NOT NULL
							THEN c.site_address_clean || ', ' || c.mailing_city || ', GA ' || c.mailing_zip5
						WHEN c.mailing_city IS NOT NULL
							THEN c.site_address_clean || ', ' || c.mailing_city || ', GA'
						WHEN c.mailing_zip5 IS NOT NULL
							THEN c.site_address_clean || ', GA ' || c.mailing_zip5
						ELSE c.site_address_clean
					END,
					c.lat,
					c.lng,
					NOW()
				FROM candidate c
				ON CONFLICT (parcel_id) DO UPDATE SET
					county_id = EXCLUDED.county_id,
					objectid = EXCLUDED.objectid,
					site_address = EXCLUDED.site_address,
					site_address_norm = EXCLUDED.site_address_norm,
					mailing_city = EXCLUDED.mailing_city,
					mailing_zip5 = EXCLUDED.mailing_zip5,
					mailing_zip4 = EXCLUDED.mailing_zip4,
					display_address = EXCLUDED.display_address,
					lat = EXCLUDED.lat,
					lng = EXCLUDED.lng,
					updated_at = NOW()
			)
			SELECT
				COALESCE((SELECT MAX(parcel_pk_id) FROM candidate), ?) AS max_id,
				(SELECT COUNT(*) FROM candidate)::int AS batch_count
		`, currentID, countyFilterID, countyFilterID, batchLimit, currentID).Scan(&result).Error; err != nil {
			totalFailed++
			_ = updateCheckpoint(gormDB, checkpointCountyName, "parcel_search", currentID, totalProcessed, totalFailed)
			return fmt.Errorf("parcel_search batch failed at id=%d: %w", currentID, err)
		}

		if result.BatchCount == 0 {
			break
		}

		currentID = result.MaxID
		totalProcessed += result.BatchCount

		if err := updateCheckpoint(gormDB, checkpointCountyName, "parcel_search", currentID, totalProcessed, totalFailed); err != nil {
			return fmt.Errorf("failed to update parcel_search checkpoint: %w", err)
		}

		log.Printf(
			"parcel_search progress county=%s processed=%d last_processed_id=%d",
			checkpointCountyName, totalProcessed, currentID,
		)
	}

	if err := completeCheckpoint(gormDB, checkpointCountyName, "parcel_search", totalProcessed, totalFailed); err != nil {
		return fmt.Errorf("failed to mark parcel_search checkpoint complete: %w", err)
	}

	log.Printf("parcel_search build complete county=%s total_processed=%d total_failed=%d", checkpointCountyName, totalProcessed, totalFailed)
	return nil
}
