package models

// ParcelClassCode holds the lookup table for parcel class codes.
type ParcelClassCode struct {
	// ID maps to smallserial (uint16)
	ID          uint16 `gorm:"primaryKey;type:smallserial"`
	CountyID    uint16 `gorm:"not null;uniqueIndex:idx_county_code"` // FK to counties.id
	Code        string `gorm:"type:varchar(10);not null;uniqueIndex:idx_county_code"`
	Description string `gorm:"type:text;not null"`
	Category    string `gorm:"type:varchar(20);not null"`
	Color       string `gorm:"type:varchar(7);default:'#888888'"`

	// Ignored by GORM because its a generated column but shown here for clarity
	// IsResidential bool `gorm:"-"`
}
