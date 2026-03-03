package models

import (
	"database/sql"
	"time"
)

// Parcel is the main record for the property's geospatial and ownership data.
type Parcel struct {
	// ID maps to bigserial (uint64)
	ID       uint64 `gorm:"primaryKey;type:bigserial"`
	CountyID uint16 `gorm:"not null;index:idx_parcel_pin"`

	// Source IDs
	ParcelID string        `gorm:"type:varchar(50);not null;uniqueIndex:idx_county_parcel"`
	ObjectID sql.NullInt64 `gorm:"type:bigint"` // objectid can be nullable

	// Data
	SiteAddress    sql.NullString  `gorm:"type:text"`
	SiteNumber     sql.NullString  `gorm:"type:text"`
	OwnerName      sql.NullString  `gorm:"type:text"`
	OwnerAddress   sql.NullString  `gorm:"type:text"`
	Acres          sql.NullFloat64 `gorm:"type:double precision"` // Acres can be nullable/null
	Classification sql.NullString  `gorm:"type:varchar(255)"`
	TaxDistrict    sql.NullString  `gorm:"type:varchar(255)"`
	SearchLat      sql.NullFloat64 `gorm:"type:real;column:search_lat"`
	SearchLng      sql.NullFloat64 `gorm:"type:real;column:search_lng"`

	// Geometry
	Geometry string `gorm:"type:geometry(MultiPolygon, 3857)"`

	// Import Tracking
	LastSync     sql.NullTime   `gorm:"type:timestamp"`
	Processed    sql.NullBool   `gorm:"type:boolean"` // NULL = success, FALSE = error/needs retry
	ErrorMessage sql.NullString `gorm:"type:text"`
	CreatedAt    time.Time      `gorm:"type:timestamp;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time      `gorm:"type:timestamp;default:CURRENT_TIMESTAMP"`

	// Non-persisted flags (used during import, not stored in DB)
	CalcAcresFromGeometry bool `gorm:"-"` // If true, calculate acres from geometry in INSERT
}

func (Parcel) TableName() string {
	return "parcels"
}
