package importers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

func StartTaxImporter(gormDB *gorm.DB, countyName string) error {
	log.Printf("Starting parcel tax import for %s county", countyName)

	// Look up county
	var county models.County
	if err := gormDB.Where("name = ?", countyName).First(&county).Error; err != nil {
		return fmt.Errorf("%s county not found: %w", countyName, err)
	}

	if !county.TaxApiUrl.Valid || county.TaxApiUrl.String == "" {
		return fmt.Errorf("%s county has no tax_api_url configured", countyName)
	}

	log.Printf("Found county: %s (ID: %d, API: %s)", county.Name, county.ID, county.TaxApiUrl.String)

	// Count all parcels in this county (efficient COUNT query)
	var parcelCount int64
	if err := gormDB.Model(&models.Parcel{}).Where("county_id = ?", county.ID).Count(&parcelCount).Error; err != nil {
		return fmt.Errorf("failed to count parcels for %s county: %w", countyName, err)
	}
	log.Printf("Found %d parcels in %s county", parcelCount, countyName)

	// Make a POST request to the tax API
	jsonData := []byte(`{
		"value": "17053301010"
	}`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "POST", county.TaxApiUrl.String, bytes.NewBuffer(jsonData))

	// Set headers to mimic browser request
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.cobbtaxpayments.org")
	req.Header.Set("Referer", "https://www.cobbtaxpayments.org/")

	resp, _ := http.DefaultClient.Do(req)

	var reader io.ReadCloser
	reader = resp.Body
	bodyBytes, readErr := io.ReadAll(reader)
	resp.Body.Close()
	if reader != resp.Body {
		reader.Close()
	}
	cancel()

	// Print response body
	if readErr != nil {
		return fmt.Errorf("failed to read tax API response: %w", readErr)
	}

	log.Printf("Response status: %s", resp.Status)
	log.Printf("Tax API response: %s", string(bodyBytes))

	// Insert into the db

	return nil
}
