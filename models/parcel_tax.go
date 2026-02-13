package models

import "time"

// ParcelTax is a single record of tax and value data for one year.
type ParcelTax struct {
	// ID maps to bigserial (uint64)
	ID       uint64 `gorm:"primaryKey;type:bigserial"`
	CountyID uint16 `gorm:"not null"`
	ParcelID uint64 `gorm:"not null;uniqueIndex:idx_tax_year"` // Foreign Key to parcels.id
	TaxYear  uint16 `gorm:"type:smallint;not null;uniqueIndex:idx_tax_year"`

	TaxAmount     *float64 `gorm:"type:numeric"`
	Appraised     *float64 `gorm:"type:numeric"`
	Assessed      *float64 `gorm:"type:numeric"`
	Millage       *float64 `gorm:"type:numeric"` // Store as decimal (e.g., 0.032)
	PayerName     string   `gorm:"type:text"`
	BillURL       string   `gorm:"type:text;column:bill_url"`
	BuildingValue *float64 `gorm:"type:numeric"`
	LandValue     *float64 `gorm:"type:numeric"`
	DueDate       string   `gorm:"type:varchar(20)"`
	PaidDate      string   `gorm:"type:varchar(20)"`
	TotalDue      *float64 `gorm:"type:numeric"`
	BackTaxes     *float64 `gorm:"type:numeric"`
	LastUpdatedDate *time.Time `gorm:"type:timestamp;column:last_updated_date"`
}
