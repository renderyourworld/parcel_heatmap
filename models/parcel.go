package models

import (
	"database/sql"
	"time"
)

// Parcel is the main record for the property's geospatial and ownership data.
type Parcel struct {
	// ID maps to bigserial (uint64)
	ID          uint64 `gorm:"primaryKey;type:bigserial"`
	CountyID    uint16 `gorm:"not null;index:idx_parcel_pin"`
	
	// Source IDs
	ParcelID    string `gorm:"type:varchar(50);not null;uniqueIndex:idx_county_parcel"`
	ObjectID    sql.NullInt64 `gorm:"type:bigint"` // objectid can be nullable
	
	// Data
	SiteAddress string `gorm:"type:text"`
	SiteNumber  sql.NullString `gorm:"type:text"`
	OwnerName   sql.NullString `gorm:"type:text"`
	OwnerAddress sql.NullString `gorm:"type:text"`
	Acres       sql.NullFloat64 `gorm:"type:numeric"` // Acres can be nullable/null
	Classification string `gorm:"type:varchar(50)"`
	TaxDistrict sql.NullString `gorm:"type:varchar(10)"`

	// Geometry
	Geometry string `gorm:"type:geometry(Polygon, 4326)"`
	CentroidGeoJSON string `gorm:"column:centroid_geojson;type:geometry(Point, 4326)"`

	// Import Tracking
	LastSync  sql.NullTime `gorm:"type:timestamp"`
	Processed bool      `gorm:"default:false"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (Parcel) TableName() string {
	return "parcels"
}
