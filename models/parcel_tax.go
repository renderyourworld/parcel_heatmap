package models

// ParcelTax is a single record of tax and value data for one year.
type ParcelTax struct {
	// ID maps to bigserial (uint64)
	ID       uint64 `gorm:"primaryKey;type:bigserial"`
	CountyID uint16 `gorm:"not null"`
	ParcelID uint64 `gorm:"not null;uniqueIndex:idx_tax_year"` // Foreign Key to parcels.id
	TaxYear  uint16 `gorm:"type:smallint;not null;uniqueIndex:idx_tax_year"`

	TaxAmount *float64 `gorm:"type:numeric"`
	Appraised *float64 `gorm:"type:numeric"`
	Assessed  *float64 `gorm:"type:numeric"`
	Millage   *float64 `gorm:"type:numeric"` // Store as decimal (e.g., 0.032)
	PayerName string   `gorm:"type:text"`
}
