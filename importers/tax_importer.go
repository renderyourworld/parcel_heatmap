package importers

import (
	"fmt"
	"log"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

func StartTaxImporter(gormDB *gorm.DB, countyName string, resume bool, maxParcels int, logging bool) error {
	log.Printf("Starting parcel tax import for %s county (resume=%v)", countyName, resume)

	// Look up county
	var county models.County
	if err := gormDB.Where("name = ?", countyName).First(&county).Error; err != nil {
		return fmt.Errorf("%s county not found: %w", countyName, err)
	}

	if !county.TaxApiUrl.Valid || county.TaxApiUrl.String == "" {
		return fmt.Errorf("%s county has no tax_api_url configured", countyName)
	}

	log.Printf("Found county: %s (ID: %d, Provider: %s, API: %s)",
		county.Name, county.ID, county.TaxProvider.String, county.TaxApiUrl.String)

	// Route based on TaxProvider type
	switch county.TaxProvider.String {
	case "GovWindow":
		return startGovWindowTaxImporter(gormDB, &county, resume, maxParcels, logging)
	case "Wildfire":
		return startWildfireTaxImporter(gormDB, &county, resume, maxParcels, logging)
	default:
		return fmt.Errorf("unknown tax provider: %s", county.TaxProvider.String)
	}
}
