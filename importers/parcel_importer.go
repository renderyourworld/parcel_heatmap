package importers

import (
	"fmt"
	"log"
	"strings"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

// Main entry point for parcel importing.
// Routes to either the GIS importer or QPublic importer based on the county's GisApiUrl.
func StartParcelImporter(gormDB *gorm.DB, countyName string, resume bool, maxParcels int, logging bool) error {
	log.Printf("Starting parcel importer for %s (resume=%v)", countyName, resume)

	// Look up county
	var county models.County
	if err := gormDB.Where("name = ?", countyName).First(&county).Error; err != nil {
		return fmt.Errorf("county '%s' not found: %w", countyName, err)
	}

	if !county.GisApiUrl.Valid || county.GisApiUrl.String == "" {
		return fmt.Errorf("county '%s' has no gis_api_url configured", countyName)
	}

	// Route based on URL type
	if strings.Contains(county.GisApiUrl.String, "qpublic.schneidercorp.com") {
		log.Printf("Detected QPublic URL - using QPublic importer")
		return startQPublicImporter(gormDB, &county, logging)
	}

	log.Printf("Using GIS importer")
	return startGISImporter(gormDB, &county, resume, maxParcels, logging)
}

// Main entry point for parcel enrichment.
// Routes to QPublic enricher if county uses QPublic, otherwise returns error.
func StartParcelEnricher(gormDB *gorm.DB, countyName string, resume bool, maxParcels int, logging bool) error {
	log.Printf("Starting parcel enricher for %s (resume=%v)", countyName, resume)

	// Look up county
	var county models.County
	if err := gormDB.Where("name = ?", countyName).First(&county).Error; err != nil {
		return fmt.Errorf("county '%s' not found: %w", countyName, err)
	}

	if !county.GisApiUrl.Valid || county.GisApiUrl.String == "" {
		return fmt.Errorf("county '%s' has no gis_api_url configured", countyName)
	}

	// Only QPublic counties support enrichment
	if !strings.Contains(county.GisApiUrl.String, "qpublic.schneidercorp.com") {
		return fmt.Errorf("county '%s' does not use QPublic - enrichment only available for QPublic counties", countyName)
	}

	log.Printf("Detected QPublic URL - using QPublic enricher")
	return startQPublicEnricher(gormDB, &county, resume, maxParcels, logging)
}
