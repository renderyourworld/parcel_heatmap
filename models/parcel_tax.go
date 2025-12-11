package models

import (
	"database/sql"
)

// ParcelTax is a single record of tax and value data for one year.
type ParcelTax struct {
	// ID maps to bigserial (uint64)
	ID       uint64 `gorm:"primaryKey;type:bigserial"`
	CountyID uint16 `gorm:"not null"`
	ParcelID uint64 `gorm:"not null;uniqueIndex:idx_tax_year"` // Foreign Key to parcels.id

	TaxYear uint16 `gorm:"type:smallint;not null;uniqueIndex:idx_tax_year"`

	// All value fields are nullable and numeric, use sql.NullFloat64
	TaxAmount        sql.NullFloat64 `gorm:"type:numeric"`
	FmvLand          sql.NullFloat64 `gorm:"type:numeric"`
	FmvBuilding      sql.NullFloat64 `gorm:"type:numeric"`
	FmvTotal         sql.NullFloat64 `gorm:"type:numeric"`
	AssessedLand     sql.NullFloat64 `gorm:"type:numeric"`
	AssessedBuilding sql.NullFloat64 `gorm:"type:numeric"`
	AssessedTotal    sql.NullFloat64 `gorm:"type:numeric"`
}
