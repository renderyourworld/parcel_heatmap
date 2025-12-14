package importers

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

const (
	// maxParcelRetries specifies how many times to retry a failed HTTP request
	maxParcelRetries = 3
	// batchSize is the number of parcels to fetch per API request
	batchSize = 1000
	// rateLimitDelay is the pause between API requests (3 seconds to be polite)
	rateLimitDelay = 3 * time.Second
)

// StartParcelImporter fetches parcel data from ArcGIS REST API using checkpoint-based resumable import
// Processes parcels in batches for efficient API usage while inserting individually for tracking errors
// maxParcels limits the total number of parcels to import (0 = no limit, useful for testing)
func StartParcelImporter(db *gorm.DB, countyName string, resume bool, maxParcels int) error {
	log.Printf("Starting parcel importer for %s (resume=%v)", countyName, resume)

	// Look up county and validate configuration
	var county models.County
	if err := db.Where("name = ?", countyName).First(&county).Error; err != nil {
		return fmt.Errorf("county '%s' not found: %w", countyName, err)
	}

	if !county.GisApiUrl.Valid || county.GisApiUrl.String == "" {
		return fmt.Errorf("county '%s' has no gis_api_url configured", countyName)
	}

	log.Printf("Found county: %s (ID: %d, API: %s)", county.Name, county.ID, county.GisApiUrl.String)

	// Load field mappings
	mappings, err := loadFieldMappings(db, county.ID)
	if err != nil {
		return fmt.Errorf("failed to load field mappings: %w", err)
	}

	log.Printf("Loaded %d field mappings", len(mappings))

	// Find the source field name for objectid
	objectIDField := "OBJECTID" // Default fallback
	for _, mapping := range mappings {
		if mapping.TargetColumn == "objectid" {
			objectIDField = mapping.SourceField
			log.Printf("Using source field '%s' for objectid ordering", objectIDField)
			break
		}
	}

	// Initialize or resume checkpoint
	checkpoint, err := initializeCheckpoint(db, countyName, resume)
	if err != nil {
		return err
	}

	log.Printf("Checkpoint initialized: last_processed_id=%d, status=%s", checkpoint.LastProcessedID, checkpoint.Status)

	// Fetch total parcel count for progress tracking
	totalCount, err := fetchTotalCount(county.GisApiUrl.String)
	if err != nil {
		log.Printf("Warning: Could not fetch total count: %v. Continuing anyway...", err)
		totalCount = -1 // Unknown
	} else {
		log.Printf("Total parcels in county: %d", totalCount)
	}

	// Process batches
	successCount := 0
	failCount := 0
	currentOffset := checkpoint.LastProcessedID

	for {
		// Rate limiting
		time.Sleep(rateLimitDelay)

		// Build query URL with proper URL encoding
		baseURL := county.GisApiUrl.String + "query"
		params := url.Values{}
		params.Set("where", fmt.Sprintf("%s > %d", objectIDField, currentOffset))
		params.Set("resultRecordCount", fmt.Sprintf("%d", batchSize))
		params.Set("outFields", "*")
		params.Set("returnGeometry", "true")
		params.Set("outSR", "4326")
		params.Set("f", "geoJson")
		params.Set("orderByFields", fmt.Sprintf("%s ASC", objectIDField))

		queryURL := baseURL + "?" + params.Encode()

		log.Printf("Fetching batch starting after %s %d...", objectIDField, currentOffset)

		// Fetch batch with retry logic
		var featureCollection models.ParcelFeatureCollection
		fetchSuccess := false

		for attempt := 1; attempt <= maxParcelRetries; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			req, _ := http.NewRequestWithContext(ctx, "GET", queryURL, nil)

			// Set headers to mimic browser request
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
			req.Header.Set("Accept", "*/*")
			req.Header.Set("Accept-Encoding", "gzip, deflate, br")
			req.Header.Set("Connection", "keep-alive")

			resp, err := http.DefaultClient.Do(req)

			if err != nil {
				cancel()
				log.Printf("Warning: Fetch error (Attempt %d/%d): %v", attempt, maxParcelRetries, err)
				if attempt < maxParcelRetries {
					time.Sleep(time.Duration(attempt) * time.Second)
					continue
				}
				return fmt.Errorf("fetch failed after %d retries: %w", maxParcelRetries, err)
			}

			// Handle gzip-compressed responses
			var reader io.ReadCloser
			switch strings.ToLower(resp.Header.Get("Content-Encoding")) {
			case "gzip":
				reader, err = gzip.NewReader(resp.Body)
				if err != nil {
					resp.Body.Close()
					cancel()
					log.Printf("Warning: Gzip decompress error (Attempt %d/%d): %v", attempt, maxParcelRetries, err)
					if attempt < maxParcelRetries {
						time.Sleep(time.Duration(attempt) * time.Second)
						continue
					}
					return fmt.Errorf("gzip decompress failed after %d retries: %w", maxParcelRetries, err)
				}
				defer reader.Close()
			default:
				reader = resp.Body
			}

			bodyBytes, readErr := io.ReadAll(reader)
			resp.Body.Close()
			if reader != resp.Body {
				reader.Close()
			}
			cancel()

			if readErr != nil {
				log.Printf("Warning: Read error (Attempt %d/%d): %v", attempt, maxParcelRetries, readErr)
				if attempt < maxParcelRetries {
					time.Sleep(time.Duration(attempt) * time.Second)
					continue
				}
				return fmt.Errorf("read failed after %d retries: %w", maxParcelRetries, readErr)
			}

			if resp.StatusCode != http.StatusOK {
				log.Printf("Warning: API returned status %d (Attempt %d/%d)", resp.StatusCode, attempt, maxParcelRetries)
				if attempt < maxParcelRetries {
					time.Sleep(time.Duration(attempt) * time.Second)
					continue
				}
				return fmt.Errorf("API returned status %d after %d retries", resp.StatusCode, maxParcelRetries)
			}

			// Check for embedded error in JSON response
			var errorCheck map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &errorCheck); err == nil {
				if errorObj, hasError := errorCheck["error"]; hasError {
					log.Printf("Warning: API returned error: %v (Attempt %d/%d)", errorObj, attempt, maxParcelRetries)
					if attempt < maxParcelRetries {
						time.Sleep(time.Duration(attempt) * time.Second)
						continue
					}
					return fmt.Errorf("API returned error after %d retries: %v", maxParcelRetries, errorObj)
				}
			}

			// Unmarshal to FeatureCollection
			if err := json.Unmarshal(bodyBytes, &featureCollection); err != nil {
				log.Printf("Warning: JSON unmarshal error (Attempt %d/%d): %v", attempt, maxParcelRetries, err)
				if attempt < maxParcelRetries {
					time.Sleep(time.Duration(attempt) * time.Second)
					continue
				}
				return fmt.Errorf("unmarshal failed after %d retries: %w", maxParcelRetries, err)
			}

			fetchSuccess = true
			break
		}

		if !fetchSuccess {
			return fmt.Errorf("failed to fetch batch after %d attempts", maxRetries)
		}

		// Check if we're done (no more features)
		if len(featureCollection.Features) == 0 {
			log.Println("No more features returned. Import complete.")
			break
		}

		log.Printf("Processing %d features in batch...", len(featureCollection.Features))

		// Process each feature individually
		batchSuccessCount := 0
		batchFailCount := 0
		maxObjectIDInBatch := currentOffset

		for _, feature := range featureCollection.Features {
			// Check if we've reached the max parcels limit (check successful inserts only)
			if maxParcels > 0 && successCount+batchSuccessCount >= maxParcels {
				log.Printf("Reached max parcels limit (%d). Stopping import.", maxParcels)
				break
			}

			// Map feature to parcel
			parcel, errMsg := mapFeatureToParcel(feature.Properties, feature.Geometry, mappings, county.ID)

			if errMsg != "" {
				// Mapping failed - insert with error tracking
				parcel.Processed = sql.NullBool{Bool: false, Valid: true} // FALSE = error
				parcel.ErrorMessage = sql.NullString{String: errMsg, Valid: true}
				batchFailCount++
				log.Printf("Failed to map parcel: %s", errMsg)
			} else {
				batchSuccessCount++
				// Processed remains NULL (zero value) = success
			}

			// Insert or update parcel
			if err := insertParcel(db, parcel); err != nil {
				log.Printf("ERROR: Failed to insert parcel %s: %v", parcel.ParcelID, err)
				batchFailCount++
				continue
			}

			// Track max OBJECTID in this batch
			if parcel.ObjectID.Valid && parcel.ObjectID.Int64 > maxObjectIDInBatch {
				maxObjectIDInBatch = parcel.ObjectID.Int64
			}
		}

		// Update checkpoint after successful batch processing
		successCount += batchSuccessCount
		failCount += batchFailCount
		currentOffset = maxObjectIDInBatch

		if err := updateCheckpoint(db, countyName, maxObjectIDInBatch, successCount, failCount); err != nil {
			log.Printf("Warning: Failed to update checkpoint: %v", err)
		}

		// Log progress
		if totalCount > 0 {
			log.Printf("Checkpoint: Processed %d of ~%d (%d failed)", successCount, totalCount, failCount)
		} else {
			log.Printf("Checkpoint: Processed %d (%d failed)", successCount, failCount)
		}

		// Check if we've reached the max parcels limit (already checked in loop, but check again in case batch ended)
		if maxParcels > 0 && successCount >= maxParcels {
			break
		}

		// If we got less than batchSize, we're likely at the end
		if len(featureCollection.Features) < batchSize {
			log.Println("Received partial batch. Likely at end of dataset.")
			break
		}
	}

	// Mark import as complete
	if err := completeCheckpoint(db, countyName, successCount, failCount); err != nil {
		log.Printf("Warning: Failed to mark checkpoint complete: %v", err)
	}

	// Calculate centroids for processed parcels
	log.Println("Calculating centroids for processed parcels...")
	if err := db.Exec(`
		UPDATE parcels 
		SET centroid = ST_Centroid(geometry) 
		WHERE county_id = ? AND centroid IS NULL AND processed IS NULL
	`, county.ID).Error; err != nil {
		log.Printf("Warning: Failed to calculate centroids: %v", err)
	} else {
		log.Println("Centroids calculated successfully")
	}

	log.Printf("Import complete! Total processed: %d, Failed: %d", successCount, failCount)

	return nil
}

// initializeCheckpoint creates or resumes a checkpoint record
func initializeCheckpoint(db *gorm.DB, countyName string, resume bool) (*models.ImportCheckpoint, error) {
	var checkpoint models.ImportCheckpoint

	err := db.Where("county_name = ?", countyName).First(&checkpoint).Error

	if err == gorm.ErrRecordNotFound {
		// No checkpoint exists - create new one
		now := time.Now()
		checkpoint = models.ImportCheckpoint{
			CountyName:      countyName,
			LastProcessedID: 0,
			Status:          "RUNNING",
			StartTime:       &now,
			TotalProcessed:  0,
			TotalFailed:     0,
		}
		if err := db.Create(&checkpoint).Error; err != nil {
			return nil, fmt.Errorf("failed to create checkpoint: %w", err)
		}
		log.Println("Created new checkpoint")
		return &checkpoint, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to query checkpoint: %w", err)
	}

	// Checkpoint exists
	if !resume && checkpoint.Status == "COMPLETE" {
		return nil, fmt.Errorf("import already completed for '%s'. Use --resume to retry failed parcels", countyName)
	}

	if resume {
		// Reset status to RUNNING for resume
		checkpoint.Status = "RUNNING"
		if err := db.Save(&checkpoint).Error; err != nil {
			return nil, fmt.Errorf("failed to update checkpoint: %w", err)
		}
		log.Printf("Resuming from checkpoint (last_processed_id=%d)", checkpoint.LastProcessedID)
	}

	return &checkpoint, nil
}

// updateCheckpoint updates the checkpoint after processing a batch
func updateCheckpoint(db *gorm.DB, countyName string, lastProcessedID int64, totalProcessed, totalFailed int) error {
	return db.Model(&models.ImportCheckpoint{}).
		Where("county_name = ?", countyName).
		Updates(map[string]interface{}{
			"last_processed_id": lastProcessedID,
			"total_processed":   totalProcessed,
			"total_failed":      totalFailed,
			"updated_at":        time.Now(),
		}).Error
}

// completeCheckpoint marks the import as complete
func completeCheckpoint(db *gorm.DB, countyName string, totalProcessed, totalFailed int) error {
	now := time.Now()
	return db.Model(&models.ImportCheckpoint{}).
		Where("county_name = ?", countyName).
		Updates(map[string]interface{}{
			"status":          "COMPLETE",
			"end_time":        &now,
			"total_processed": totalProcessed,
			"total_failed":    totalFailed,
			"updated_at":      now,
		}).Error
}

// fetchTotalCount queries the API for total parcel count
func fetchTotalCount(baseURL string) (int, error) {
	url := fmt.Sprintf("%squery?where=1=1&returnCountOnly=true&f=json", baseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Count int `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.Count, nil
}

// insertParcel inserts or updates a parcel in the database
func insertParcel(db *gorm.DB, parcel *models.Parcel) error {
	// Use raw SQL for ON CONFLICT handling with PostGIS geometry
	sql := `
		INSERT INTO parcels (
			county_id, parcel_id, objectid, 
			site_address, site_number, owner_name, owner_address,
			acres, classification, tax_district,
			geometry, processed, error_message, last_sync, created_at, updated_at
		) VALUES (
			$1, $2, $3,
			$4, $5, $6, $7,
			$8, $9, $10,
			ST_SetSRID(ST_GeomFromGeoJSON($11), 4326), $12, $13, NOW(), NOW(), NOW()
		)
		ON CONFLICT (county_id, parcel_id) DO UPDATE SET
			objectid = EXCLUDED.objectid,
			site_address = EXCLUDED.site_address,
			site_number = EXCLUDED.site_number,
			owner_name = EXCLUDED.owner_name,
			owner_address = EXCLUDED.owner_address,
			acres = EXCLUDED.acres,
			classification = EXCLUDED.classification,
			tax_district = EXCLUDED.tax_district,
			geometry = EXCLUDED.geometry,
			processed = EXCLUDED.processed,
			error_message = EXCLUDED.error_message,
			last_sync = NOW(),
			updated_at = NOW()
	`

	return db.Exec(sql,
		parcel.CountyID,
		parcel.ParcelID,
		parcel.ObjectID,
		parcel.SiteAddress,
		parcel.SiteNumber,
		parcel.OwnerName,
		parcel.OwnerAddress,
		parcel.Acres,
		parcel.Classification,
		parcel.TaxDistrict,
		parcel.Geometry,
		parcel.Processed,
		parcel.ErrorMessage,
	).Error
}
