package importers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/renderyourworld/parcel_heatmap/models"
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxTaxRetries     = 3
	taxBatchSize      = 100
	taxRateLimitDelay = 3 * time.Second
)

type WildfireTaxResponse struct {
	TotalRecords int `json:"TotalRecords"`
	Records      []struct {
		District string `json:"District"`
		Year     uint16 `json:"Year"`
		Values   struct {
			Appraised float64 `json:"Appraised"`
			Assessed  float64 `json:"Assessed"`
			BaseTax   float64 `json:"BaseTax"`
		} `json:"Values"`
		Payment struct {
			Status    string `json:"Status"`
			PayerName string `json:"PayerName"`
		} `json:"Payment"`
	} `json:"Records"`
}

// ErrRateLimit is returned when the API returns a 429 status after retries
var ErrRateLimit = fmt.Errorf("API rate limit exceeded")

func fetchTaxData(apiURL string, parcelID string) (*WildfireTaxResponse, error) {
	jsonData := []byte(fmt.Sprintf(`{"value": "%s"}`, parcelID))

	var bodyBytes []byte
	var err error

	for attempt := 1; attempt <= maxTaxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))

		// Set headers to mimic browser request
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")

		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			cancel()
			log.Printf("Warning: Tax API fetch error (Attempt %d/%d): %v", attempt, maxTaxRetries, doErr)
			if attempt < maxTaxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
			return nil, doErr
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			cancel()
			if attempt < maxTaxRetries {
				backoff := time.Duration(attempt*5) * time.Second
				log.Printf("Warning: Tax API returned status 429. Waiting %v before retry %d/%d...", backoff, attempt+1, maxTaxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, ErrRateLimit
		}

		bodyBytes, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if err != nil {
			log.Printf("Warning: Tax API read error (Attempt %d/%d): %v", attempt, maxTaxRetries, err)
			if attempt < maxTaxRetries {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			log.Printf("Warning: Tax API returned status %d (Attempt %d/%d)", resp.StatusCode, attempt, maxTaxRetries)
			if attempt < maxTaxRetries {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
		}

		var wildfireData WildfireTaxResponse
		if err := json.Unmarshal(bodyBytes, &wildfireData); err != nil {
			log.Printf("Warning: Tax API unmarshal error (Attempt %d/%d): %v", attempt, maxTaxRetries, err)
			if attempt < maxTaxRetries {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return nil, err
		}

		return &wildfireData, nil
	}

	return nil, fmt.Errorf("failed after %d retries", maxTaxRetries)
}

func processTaxRecords(db *gorm.DB, parcel *models.Parcel, data *WildfireTaxResponse, countyID uint16) error {
	// Track tax years seen in this response to handle duplicates
	seenYears := make(map[uint16]float64)
	var taxDistrict string

	err := db.Transaction(func(tx *gorm.DB) error {
		for _, record := range data.Records {
			// Duplicate handling: If we've seen this year, skip if baseTax is the same
			if prevBaseTax, exists := seenYears[record.Year]; exists {
				if prevBaseTax == record.Values.BaseTax {
					continue
				}
			}
			seenYears[record.Year] = record.Values.BaseTax

			// Calculate millage
			millage := 0.0
			if record.Values.Assessed > 0 {
				millage = record.Values.BaseTax / record.Values.Assessed
			}

			// Prepare tax record
			taxRecord := models.ParcelTax{
				CountyID:  countyID,
				ParcelID:  parcel.ID,
				TaxYear:   record.Year,
				TaxAmount: &record.Values.BaseTax,
				Appraised: &record.Values.Appraised,
				Assessed:  &record.Values.Assessed,
				Millage:   &millage,
				PayerName: record.Payment.PayerName,
			}

			// Upsert tax record using GORM Clause
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "parcel_id"}, {Name: "tax_year"}},
				DoUpdates: clause.AssignmentColumns([]string{"tax_amount", "appraised", "assessed", "millage", "payer_name"}),
			}).Create(&taxRecord).Error; err != nil {
				return err
			}

			// Update taxDistrict if record.District is not empty
			if record.District != "" {
				taxDistrict = record.District
			}
		}

		// Update parcel tax_district logic
		if taxDistrict != "" {
			currentDistrict := ""
			if parcel.TaxDistrict.Valid {
				currentDistrict = parcel.TaxDistrict.String
			}

			if taxDistrict == currentDistrict {
				return nil
			}

			if currentDistrict != "" && strings.HasPrefix(taxDistrict, currentDistrict) {
				parcel.TaxDistrict = sql.NullString{String: taxDistrict, Valid: true}
			} else if currentDistrict == "" {
				parcel.TaxDistrict = sql.NullString{String: taxDistrict, Valid: true}
			} else {
				parcel.TaxDistrict = sql.NullString{String: currentDistrict + " " + taxDistrict, Valid: true}
			}

			// Save parcel changes
			return tx.Model(parcel).Select("TaxDistrict").Updates(parcel).Error
		}

		return nil
	})

	return err
}

func startWildfireTaxImporter(gormDB *gorm.DB, county *models.County, resume bool, maxParcels int, logging bool) error {
	// Initialize or resume checkpoint
	checkpoint, err := initializeCheckpoint(gormDB, county.Name, "tax", resume)
	if err != nil {
		return err
	}

	// Initialize performance logger
	perfLogger := utils.NewPerfLogger(logging)

	// Fetch total parcel count for progress tracking
	var totalParcels int64
	if err := gormDB.Model(&models.Parcel{}).Where("county_id = ?", county.ID).Count(&totalParcels).Error; err != nil {
		log.Printf("Warning: Could not fetch total count: %v. Continuing anyway...", err)
		totalParcels = -1
	} else {
		log.Printf("Total parcels to process in county: %d", totalParcels)
	}

	successCount := 0
	failCount := 0
	currentID := uint64(checkpoint.LastProcessedID)
	stopImport := false

	for {
		// Fetch batch of parcels
		var parcels []models.Parcel
		if err := gormDB.Where("county_id = ? AND id > ?", county.ID, currentID).
			Order("id ASC").
			Limit(taxBatchSize).
			Find(&parcels).Error; err != nil {
			return fmt.Errorf("failed to fetch parcels batch: %w", err)
		}

		if len(parcels) == 0 {
			log.Println("No more parcels to process. Import complete.")
			break
		}

		for _, parcel := range parcels {
			// Check if we've reached the max parcels limit
			if maxParcels > 0 && successCount >= maxParcels {
				log.Printf("Reached max parcels limit (%d). Stopping import.", maxParcels)
				stopImport = true
				break
			}

			// Rate limiting with jitter (3-5 seconds)
			jitter := time.Duration(rand.Intn(2000)) * time.Millisecond
			time.Sleep(taxRateLimitDelay + jitter)

			// Fetch tax data
			taxData, err := fetchTaxData(county.TaxApiUrl.String, parcel.ParcelID)
			if err != nil {
				if errors.Is(err, ErrRateLimit) {
					log.Printf("CRITICAL: API rate limit reached for parcel %s. Stopping import to avoid lockout.", parcel.ParcelID)
					stopImport = true
					break
				}
				log.Printf("ERROR: Failed to fetch tax data for parcel %s (ID %d): %v", parcel.ParcelID, parcel.ID, err)
				failCount++
				currentID = parcel.ID
				continue
			}

			// Process and Save tax records
			if err := processTaxRecords(gormDB, &parcel, taxData, county.ID); err != nil {
				log.Printf("ERROR: Failed to process tax records for parcel %s: %v", parcel.ParcelID, err)
				failCount++
			} else {
				successCount++
			}

			currentID = parcel.ID

			// Update performance logger
			perfLogger.Update(successCount, 10*time.Second)
		}

		// Update checkpoint after batch
		if err := updateCheckpoint(gormDB, county.Name, "tax", int64(currentID), successCount, failCount); err != nil {
			log.Printf("Warning: Failed to update checkpoint: %v", err)
		}

		log.Printf("Progress: %d/%d processed (%d failed)", successCount, totalParcels, failCount)

		if stopImport {
			break
		}
	}

	// Mark import as complete
	if err := completeCheckpoint(gormDB, county.Name, "tax", successCount, failCount); err != nil {
		log.Printf("Warning: Failed to mark checkpoint complete: %v", err)
	}

	log.Printf("Tax import complete! Total processed: %d, Failed: %d", successCount, failCount)
	perfLogger.LogFinal()

	return nil
}
