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
	Acres          sql.NullFloat64 `gorm:"type:numeric"` // Acres can be nullable/null
	Classification sql.NullString  `gorm:"type:varchar(50)"`
	TaxDistrict    sql.NullString  `gorm:"type:varchar(10)"`

	// Geometry
	Geometry        string `gorm:"type:geometry(MultiPolygon, 4326)"`
	Centroid        string `gorm:"column:centroid;type:geometry(Point, 4326)"`
	BoundaryGeoJSON string `gorm:"column:boundary_geojson;type:jsonb"`

	// Import Tracking
	LastSync     sql.NullTime   `gorm:"type:timestamp"`
	Processed    sql.NullBool   `gorm:"type:boolean"` // NULL = success, FALSE = error/needs retry
	ErrorMessage sql.NullString `gorm:"type:text"`
	CreatedAt    time.Time      `gorm:"type:timestamp;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time      `gorm:"type:timestamp;default:CURRENT_TIMESTAMP"`
}

func (Parcel) TableName() string {
	return "parcels"
}
