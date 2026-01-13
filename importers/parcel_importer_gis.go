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
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
)

const (
	maxParcelRetries = 5
	defaultBatchSize = 1000
	rateLimitDelay   = 3 * time.Second
)

// Creates or resumes a checkpoint record
func initializeCheckpoint(db *gorm.DB, countyName string, importType string, resume bool) (*models.ImportCheckpoint, error) {
	var checkpoint models.ImportCheckpoint

	err := db.Where("county_name = ? AND import_type = ?", countyName, importType).First(&checkpoint).Error

	if err == gorm.ErrRecordNotFound {
		// No checkpoint exists - create new one
		now := time.Now()
		checkpoint = models.ImportCheckpoint{
			CountyName:      countyName,
			ImportType:      importType,
			LastProcessedID: 0,
			Status:          "RUNNING",
			StartTime:       &now,
			TotalProcessed:  0,
			TotalFailed:     0,
		}
		if err := db.Create(&checkpoint).Error; err != nil {
			return nil, fmt.Errorf("failed to create checkpoint: %w", err)
		}
		log.Printf("Created new %s checkpoint for %s", importType, countyName)
		return &checkpoint, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to query checkpoint: %w", err)
	}

	// Checkpoint exists
	if !resume && checkpoint.Status == "COMPLETE" {
		return nil, fmt.Errorf("%s import already completed for '%s'. Use --resume to retry failed records", importType, countyName)
	}

	if resume {
		// Reset status to RUNNING for resume
		checkpoint.Status = "RUNNING"
		if err := db.Save(&checkpoint).Error; err != nil {
			return nil, fmt.Errorf("failed to update checkpoint: %w", err)
		}
		log.Printf("Resuming %s import from checkpoint (last_processed_id=%d)", importType, checkpoint.LastProcessedID)
	}

	return &checkpoint, nil
}

// Updates the checkpoint after processing a batch
func updateCheckpoint(db *gorm.DB, countyName string, importType string, lastProcessedID int64, totalProcessed, totalFailed int) error {
	return db.Model(&models.ImportCheckpoint{}).
		Where("county_name = ? AND import_type = ?", countyName, importType).
		Updates(map[string]interface{}{
			"last_processed_id": lastProcessedID,
			"total_processed":   totalProcessed,
			"total_failed":      totalFailed,
			"updated_at":        time.Now(),
		}).Error
}

// Marks the import as complete
func completeCheckpoint(db *gorm.DB, countyName string, importType string, totalProcessed, totalFailed int) error {
	now := time.Now()
	return db.Model(&models.ImportCheckpoint{}).
		Where("county_name = ? AND import_type = ?", countyName, importType).
		Updates(map[string]interface{}{
			"status":          "COMPLETE",
			"end_time":        &now,
			"total_processed": totalProcessed,
			"total_failed":    totalFailed,
			"updated_at":      now,
		}).Error
}

// Queries the API for total parcel count
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

// Inserts or updates a parcel in the database
func insertParcel(db *gorm.DB, parcel *models.Parcel) error {
	// Use raw SQL for ON CONFLICT handling with PostGIS geometry
	// Note: Uses (county_id, objectid) as unique constraint since parcel_id can have duplicates

	var sqlStmt string
	var args []interface{}

	if parcel.CalcAcresFromGeometry {
		// Calculate acres from geometry using ST_Area with geography conversion
		// ST_Area(geometry::geography) returns square meters, divide by 4046.86 for acres
		sqlStmt = `
			INSERT INTO parcels (
				county_id, parcel_id, objectid, 
				site_address, site_number, owner_name, owner_address,
				acres, classification, tax_district,
				geometry, processed, error_message, last_sync, created_at, updated_at
			) VALUES (
				$1, $2, $3,
				$4, $5, $6, $7,
				ROUND((ST_Area(ST_Transform(ST_SetSRID(ST_GeomFromGeoJSON($8), 3857), 4326)::geography) / 4046.86)::numeric, 2),
				$9, $10,
				ST_SetSRID(ST_GeomFromGeoJSON($8), 3857), $11, $12, NOW(), NOW(), NOW()
			)
			ON CONFLICT (county_id, objectid) DO UPDATE SET
				parcel_id = EXCLUDED.parcel_id,
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
		args = []interface{}{
			parcel.CountyID,
			parcel.ParcelID,
			parcel.ObjectID,
			parcel.SiteAddress,
			parcel.SiteNumber,
			parcel.OwnerName,
			parcel.OwnerAddress,
			parcel.Geometry, // $8 - used for both acres calculation and geometry column
			parcel.Classification,
			parcel.TaxDistrict,
			parcel.Processed,
			parcel.ErrorMessage,
		}
	} else {
		// Standard insert with provided acres value
		sqlStmt = `
			INSERT INTO parcels (
				county_id, parcel_id, objectid, 
				site_address, site_number, owner_name, owner_address,
				acres, classification, tax_district,
				geometry, processed, error_message, last_sync, created_at, updated_at
			) VALUES (
				$1, $2, $3,
				$4, $5, $6, $7,
				$8, $9, $10,
				ST_SetSRID(ST_GeomFromGeoJSON($11), 3857), $12, $13, NOW(), NOW(), NOW()
			)
			ON CONFLICT (county_id, objectid) DO UPDATE SET
				parcel_id = EXCLUDED.parcel_id,
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
		args = []interface{}{
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
		}
	}

	return db.Exec(sqlStmt, args...).Error
}

// Fallback import process for servers that don't support pagination.
// It fetches all ObjectIDs first, then fetches full records in batches using an IN clause.
func fetchAllByIDs(gormDB *gorm.DB, county *models.County, mappings []models.CountyFieldMapping, checkpoint *models.ImportCheckpoint, initialSuccess, initialFail int, batchSize int, maxParcels int, logging bool, objectIDField string, useEsriJSON bool) error {
	log.Printf("Starting ID-based fallback fetch for %s...", county.Name)

	// Fetch all IDs
	baseURL := county.GisApiUrl.String + "query"
	params := url.Values{}
	params.Set("where", "1=1")
	params.Set("returnIdsOnly", "true")
	params.Set("f", "json")

	queryURL := baseURL + "?" + params.Encode()
	log.Printf("Fetching all IDs from: %s", queryURL)

	req, _ := http.NewRequest("GET", queryURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch IDs: %w", err)
	}
	defer resp.Body.Close()

	var idResponse struct {
		ObjectIDs []int64 `json:"objectIds"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&idResponse); err != nil {
		return fmt.Errorf("failed to decode ID response: %w", err)
	}

	if idResponse.Error != nil {
		return fmt.Errorf("API error fetching IDs: %s", idResponse.Error.Message)
	}

	totalIDs := len(idResponse.ObjectIDs)
	log.Printf("Found %d total IDs for county", totalIDs)

	// Filter IDs in-place to avoid duplication
	n := 0
	for _, id := range idResponse.ObjectIDs {
		if id > checkpoint.LastProcessedID {
			idResponse.ObjectIDs[n] = id
			n++
		}
	}
	remainingIDs := idResponse.ObjectIDs[:n]

	log.Printf("Processing %d remaining IDs starting after %d", len(remainingIDs), checkpoint.LastProcessedID)

	successCount := initialSuccess
	failCount := initialFail
	perfLogger := utils.NewPerfLogger(logging)

	// Fetch records in batches
	for i := 0; i < len(remainingIDs); i += batchSize {
		end := i + batchSize
		if end > len(remainingIDs) {
			end = len(remainingIDs)
		}

		batchIDs := remainingIDs[i:end]
		idStrings := make([]string, len(batchIDs))
		for j, id := range batchIDs {
			idStrings[j] = fmt.Sprintf("%d", id)
		}

		idList := strings.Join(idStrings, ",")
		whereClause := fmt.Sprintf("%s IN (%s)", objectIDField, idList)

		params := url.Values{}
		params.Set("where", whereClause)
		params.Set("outFields", "*")
		params.Set("returnGeometry", "true")
		params.Set("outSR", "3857")
		if useEsriJSON {
			params.Set("f", "json")
		} else {
			params.Set("f", "geoJson")
		}

		log.Printf("Fetching batch %d-%d/%d...", i+1, end, len(remainingIDs))

		var featureCollection models.ParcelFeatureCollection
		// Use POST to avoid potential URL length limits
		batchReq, _ := http.NewRequest("POST", baseURL, strings.NewReader(params.Encode()))
		batchReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
		batchReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		batchReq.Header.Set("Accept", "application/json, text/plain, */*")
		batchReq.Header.Set("Connection", "keep-alive")

		batchResp, err := http.DefaultClient.Do(batchReq)
		if err != nil {
			return fmt.Errorf("batch fetch failed: %w", err)
		}

		bodyBytes, readErr := io.ReadAll(batchResp.Body)
		batchResp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("failed to read batch response: %w", readErr)
		}

		if batchResp.StatusCode != http.StatusOK {
			return fmt.Errorf("batch fetch returned status %d: %s", batchResp.StatusCode, string(bodyBytes))
		}

		if useEsriJSON {
			var esriCollection models.EsriJSONFeatureCollection
			if err := json.Unmarshal(bodyBytes, &esriCollection); err != nil {
				snippet := string(bodyBytes)
				if len(snippet) > 200 {
					snippet = snippet[:200] + "..."
				}
				log.Printf("DEBUG: Response body snippet: %s", snippet)
				return fmt.Errorf("failed to decode Esri batch JSON: %w", err)
			}
			// Convert Esri to ParcelFeatureCollection
			featureCollection = models.ParcelFeatureCollection{
				Type:     "FeatureCollection",
				Features: make([]models.ParcelFeature, len(esriCollection.Features)),
			}
			for idx, ef := range esriCollection.Features {
				featureCollection.Features[idx] = models.ParcelFeature{
					Type:       "Feature",
					Properties: ef.Attributes,
					Geometry:   ef.Geometry.ToGeoJSONGeometry(),
				}
			}
		} else {
			if err := json.Unmarshal(bodyBytes, &featureCollection); err != nil {
				snippet := string(bodyBytes)
				if len(snippet) > 200 {
					snippet = snippet[:200] + "..."
				}
				log.Printf("DEBUG: Response body snippet: %s", snippet)
				return fmt.Errorf("failed to decode batch JSON: %w", err)
			}
		}

		// Process features
		maxObjectIDInBatch := checkpoint.LastProcessedID
		for _, feature := range featureCollection.Features {
			// Check if we've reached the max parcels limit
			if maxParcels > 0 && successCount >= maxParcels {
				log.Printf("Reached max parcels limit (%d) in ID-based fallback. Stopping.", maxParcels)
				break
			}

			parcel, errMsg := mapFeatureToParcel(feature.Properties, feature.Geometry, mappings, county.ID)
			if errMsg != "" {
				parcel.Processed = sql.NullBool{Bool: false, Valid: true}
				parcel.ErrorMessage = sql.NullString{String: errMsg, Valid: true}
			}

			if err := insertParcel(gormDB, parcel); err != nil {
				log.Printf("ERROR: Failed to insert parcel %s: %v", parcel.ParcelID, err)
				failCount++
				continue
			}

			if !parcel.Processed.Bool && parcel.Processed.Valid {
				failCount++
			} else {
				successCount++
			}

			if parcel.ObjectID.Valid && parcel.ObjectID.Int64 > maxObjectIDInBatch {
				maxObjectIDInBatch = parcel.ObjectID.Int64
			}
		}

		// Update checkpoint
		if err := updateCheckpoint(gormDB, county.Name, "parcel", maxObjectIDInBatch, successCount, failCount); err != nil {
			log.Printf("Warning: Failed to update checkpoint: %v", err)
		}

		perfLogger.Update(successCount, 10*time.Second)
		log.Printf("Progress: %d/%d processed (%d failed)", successCount, totalIDs, failCount)

		// Check if we've reached the max parcels limit (after batch ended)
		if maxParcels > 0 && successCount >= maxParcels {
			break
		}

		// Rate limit
		time.Sleep(rateLimitDelay)
	}

	log.Printf("ID-based import complete! Success: %d, Fail: %d", successCount, failCount)
	perfLogger.LogFinal()
	return nil
}

// Fetches parcel data from ArcGIS REST API using checkpoint-based resumable import
func startGISImporter(gormDB *gorm.DB, county *models.County, resume bool, maxParcels int, logging bool) error {
	log.Printf("Starting GIS importer for %s (resume=%v)", county.Name, resume)
	log.Printf("Found county: %s (ID: %d, API: %s)", county.Name, county.ID, county.GisApiUrl.String)

	// Load field mappings
	mappings, err := loadFieldMappings(gormDB, county.ID)
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
	checkpoint, err := initializeCheckpoint(gormDB, county.Name, "parcel", resume)
	if err != nil {
		return err
	}

	log.Printf("Checkpoint initialized: last_processed_id=%d, status=%s", checkpoint.LastProcessedID, checkpoint.Status)

	// Initialize performance logger
	perfLogger := utils.NewPerfLogger(logging)
	if logging {
		log.Println("Performance logging enabled")
	}

	// Fetch total parcel count for progress tracking
	totalCount, err := fetchTotalCount(county.GisApiUrl.String)
	if err != nil {
		log.Printf("Warning: Could not fetch total count: %v. Continuing anyway...", err)
		totalCount = -1 // Unknown
	} else {
		log.Printf("Total parcels in county: %d", totalCount)
	}

	// Use county-specific batch size if available, otherwise default to 1000
	dynamicBatchSize := int(county.MaxRecordCount)
	if dynamicBatchSize <= 0 {
		dynamicBatchSize = defaultBatchSize
	}

	log.Printf("Starting parcel import for %s county (resume=%v, batchSize=%d)", county.Name, resume, dynamicBatchSize)

	// Process batches
	successCount := 0
	failCount := 0
	currentOffset := checkpoint.LastProcessedID
	useEsriJSON := false

	for {
		// Rate limiting
		time.Sleep(rateLimitDelay)

		// Build query URL with proper URL encoding
		baseURL := county.GisApiUrl.String + "query"
		params := url.Values{}
		params.Set("where", fmt.Sprintf("%s > %d", objectIDField, currentOffset))
		params.Set("resultRecordCount", fmt.Sprintf("%d", dynamicBatchSize))
		params.Set("outFields", "*")
		params.Set("returnGeometry", "true")
		params.Set("outSR", "3857")

		if useEsriJSON {
			params.Set("f", "json")
		} else {
			params.Set("f", "geoJson")
		}

		params.Set("orderByFields", fmt.Sprintf("%s ASC", objectIDField))

		queryURL := baseURL + "?" + params.Encode()

		log.Printf("Fetching batch starting after %s %d...", objectIDField, currentOffset)
		log.Printf("Full Query URL: %s", queryURL)

		// Fetch batch with retry logic
		var featureCollection models.ParcelFeatureCollection
		fetchSuccess := false
		needBatchRetry := false

		for attempt := 1; attempt <= maxParcelRetries; attempt++ {
			// Refresh query URL in case dynamicBatchSize changed
			params.Set("resultRecordCount", fmt.Sprintf("%d", dynamicBatchSize))
			queryURL = baseURL + "?" + params.Encode()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			req, _ := http.NewRequestWithContext(ctx, "GET", queryURL, nil)

			// Set headers to mimic browser request
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
			req.Header.Set("Accept", "application/json, text/plain, */*")
			req.Header.Set("Connection", "keep-alive")

			resp, err := http.DefaultClient.Do(req)

			if err != nil {
				cancel()
				log.Printf("Warning: Fetch error (Attempt %d/%d): %v", attempt, maxParcelRetries, err)
				if attempt < maxParcelRetries {
					// Reduce batch size on timeout or connection error
					if dynamicBatchSize > 100 {
						dynamicBatchSize /= 2
						log.Printf("Reducing batch size to %d due to error...", dynamicBatchSize)
					}
					time.Sleep(time.Duration(attempt) * 2 * time.Second)
					continue
				}
				return fmt.Errorf("fetch failed after %d retries: %w", maxParcelRetries, err)
			}

			// Debug info
			contentType := resp.Header.Get("Content-Type")
			contentEnc := resp.Header.Get("Content-Encoding")

			// Handle decompression manually only if necessary, but Go's DefaultClient
			// usually handles this automatically if we DON'T set Accept-Encoding.
			reader := resp.Body
			if strings.ToLower(contentEnc) == "gzip" {
				gzReader, err := gzip.NewReader(resp.Body)
				if err == nil {
					reader = gzReader
					defer gzReader.Close()
				}
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

			// Check for 400 Bad Request which usually indicates format not supported or invalid params
			if resp.StatusCode == http.StatusBadRequest {
				if !useEsriJSON {
					log.Printf("Received 400 Bad Request with f=geoJson. Attempting to switch to f=json (Esri JSON)...")
					useEsriJSON = true
					needBatchRetry = true
					break // Break fetch loop to restart batch loop
				}
			}

			// Check for embedded error in JSON response first (even on non-200 status)
			var errorCheck map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &errorCheck); err == nil {
				if errorObj, hasError := errorCheck["error"]; hasError {
					errMap, isMap := errorObj.(map[string]interface{})
					if isMap {
						// Check for pagination error
						errMsg, _ := errMap["message"].(string)
						if strings.Contains(errMsg, "Pagination is not supported") {
							log.Printf("Pagination not supported by this server. Switching to ID-based fallback...")
							return fetchAllByIDs(gormDB, county, mappings, checkpoint, successCount, failCount, dynamicBatchSize, maxParcels, logging, objectIDField, useEsriJSON)
						}

						// Check if error implies we should try switching format
						if !useEsriJSON {
							log.Printf("API Error with f=geoJson. Switching to f=json...")
							useEsriJSON = true
							needBatchRetry = true
							break
						}
					}

					log.Printf("Warning: API returned error: %v (Attempt %d/%d)", errorObj, attempt, maxParcelRetries)
					if attempt < maxParcelRetries {
						time.Sleep(time.Duration(attempt) * 2 * time.Second)
						continue
					}
					return fmt.Errorf("API returned error after %d retries: %v", maxParcelRetries, errorObj)
				}
			}

			if resp.StatusCode != http.StatusOK {
				log.Printf("Warning: API returned status %d (Attempt %d/%d)", resp.StatusCode, attempt, maxParcelRetries)
				if attempt < maxParcelRetries {
					if dynamicBatchSize > 100 {
						dynamicBatchSize /= 2
						log.Printf("Reducing batch size to %d due to status %d...", dynamicBatchSize, resp.StatusCode)
					}
					time.Sleep(time.Duration(attempt) * 2 * time.Second)
					continue
				}
				return fmt.Errorf("API returned status %d after %d retries", resp.StatusCode, maxParcelRetries)
			}

			// Unmarshal logic based on format
			if useEsriJSON {
				var esriCollection models.EsriJSONFeatureCollection
				if err := json.Unmarshal(bodyBytes, &esriCollection); err != nil {
					log.Printf("Warning: Esri JSON unmarshal error (Attempt %d/%d): %v", attempt, maxParcelRetries, err)
					if attempt < maxParcelRetries {
						time.Sleep(time.Duration(attempt) * time.Second)
						continue
					}
					return fmt.Errorf("esri unmarshal failed after %d retries: %w", maxParcelRetries, err)
				}

				// Convert Esri Collection to ParcelFeatureCollection
				featureCollection = models.ParcelFeatureCollection{
					Type:     "FeatureCollection",
					Features: make([]models.ParcelFeature, len(esriCollection.Features)),
				}

				for i, ef := range esriCollection.Features {
					featureCollection.Features[i] = models.ParcelFeature{
						Type:       "Feature",
						Properties: ef.Attributes,
						Geometry:   ef.Geometry.ToGeoJSONGeometry(),
					}
				}

			} else {
				// Standard GeoJSON
				if err := json.Unmarshal(bodyBytes, &featureCollection); err != nil {
					bodySnippet := string(bodyBytes)
					if len(bodySnippet) > 100 {
						bodySnippet = bodySnippet[:100]
					}
					log.Printf("DEBUG: Content-Type: %s, Content-Encoding: %s", contentType, contentEnc)
					log.Printf("DEBUG: Body snippet: %s", bodySnippet)
					log.Printf("Warning: JSON unmarshal error (Attempt %d/%d): %v", attempt, maxParcelRetries, err)
					if attempt < maxParcelRetries {
						time.Sleep(time.Duration(attempt) * time.Second)
						continue
					}
					return fmt.Errorf("unmarshal failed after %d retries: %w", maxParcelRetries, err)
				}
			}

			fetchSuccess = true
			break
		}

		if needBatchRetry {
			continue // Restart the main loop to rebuild URL with new params
		}

		if !fetchSuccess {
			return fmt.Errorf("failed to fetch batch after %d attempts", maxParcelRetries)
		}

		// Check if we're done (no more features)
		if len(featureCollection.Features) == 0 {
			log.Println("No more features returned. Import complete.")
			break
		}

		// Map all features to parcels first
		parcels := make([]*models.Parcel, 0, len(featureCollection.Features))
		for _, feature := range featureCollection.Features {
			// Check if we've reached the max parcels limit
			if maxParcels > 0 && successCount+len(parcels) >= maxParcels {
				log.Printf("Reached max parcels limit (%d). Stopping import.", maxParcels)
				break
			}

			// Map feature to parcel
			parcel, errMsg := mapFeatureToParcel(feature.Properties, feature.Geometry, mappings, county.ID)

			if errMsg != "" {
				// Mapping failed - mark with error
				parcel.Processed = sql.NullBool{Bool: false, Valid: true}
				parcel.ErrorMessage = sql.NullString{String: errMsg, Valid: true}
				log.Printf("Failed to map parcel: %s", errMsg)
			}

			parcels = append(parcels, parcel)
		}

		// Insert parcels one at a time for error tracking
		batchSuccessCount := 0
		batchFailCount := 0
		maxObjectIDInBatch := currentOffset

		for _, parcel := range parcels {
			if err := insertParcel(gormDB, parcel); err != nil {
				log.Printf("ERROR: Failed to insert parcel %s: %v", parcel.ParcelID, err)
				batchFailCount++
				continue
			}

			// Track success/fail based on mapping errors
			if parcel.Processed.Valid && !parcel.Processed.Bool {
				batchFailCount++
			} else {
				batchSuccessCount++
			}

			// Track max OBJECTID
			if parcel.ObjectID.Valid && parcel.ObjectID.Int64 > maxObjectIDInBatch {
				maxObjectIDInBatch = parcel.ObjectID.Int64
			}
		}

		// Update checkpoint after successful batch processing
		successCount += batchSuccessCount
		failCount += batchFailCount
		currentOffset = maxObjectIDInBatch

		if err := updateCheckpoint(gormDB, county.Name, "parcel", maxObjectIDInBatch, successCount, failCount); err != nil {
			log.Printf("Warning: Failed to update checkpoint: %v", err)
		}

		// Update performance logger
		perfLogger.Update(successCount, 10*time.Second)

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
		if len(featureCollection.Features) < dynamicBatchSize {
			log.Println("Received partial batch. Likely at end of dataset.")
			break
		}
	}

	// Mark import as complete
	if err := completeCheckpoint(gormDB, county.Name, "parcel", successCount, failCount); err != nil {
		log.Printf("Warning: Failed to mark checkpoint complete: %v", err)
	}

	log.Printf("Parcel import complete! Total processed: %d, Failed: %d", successCount, failCount)

	// Log final performance summary
	perfLogger.LogFinal()

	return nil
}
