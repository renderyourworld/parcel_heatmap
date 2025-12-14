package models

import (
	"database/sql"
)

// County represents a single Georgia county with boundary geometry and demographic data.
// Geometry fields are stored as PostGIS geometry types and accessed via raw SQL queries.
// The precomputed JSONB columns (BoundaryGeojson, BoundarySimplifiedGeojson) are
// maintained by database triggers to avoid expensive ST_AsGeoJSON calls per-request.
type County struct {
	// ID maps to PostgreSQL smallserial (uint16 is sufficient for 159 GA counties)
	ID          uint16          `gorm:"primaryKey;type:smallserial"`
	Name        string          `gorm:"type:varchar(50);not null;unique"`
	State       string          `gorm:"type:varchar(2);default:'GA'"`
	Population  sql.NullInt64   `gorm:"type:int"`
	Region      sql.NullString  `gorm:"type:varchar(50)"`
	Acres       sql.NullFloat64 `gorm:"type:numeric"`
	SquareMiles sql.NullFloat64 `gorm:"type:numeric"`

	// External API URLs for additional data sources
	GisApiUrl sql.NullString `gorm:"type:text"`
	TaxApiUrl sql.NullString `gorm:"type:text"`

	// Geometry fields. PostGIS handles the spatial type
	Boundary           string `gorm:"type:geometry(MultiPolygon, 4326);not null"`
	BoundarySimplified string `gorm:"type:geometry(MultiPolygon, 4326)"`
	Centroid           string `gorm:"type:geometry(Point, 4326)"`
	BBox               string `gorm:"type:geometry(Polygon, 4326)"`

	// Precomputed GeoJSON columns (maintained by database trigger)
	// These columns store pre-rendered Feature JSON objects, eliminating the need for
	// expensive per-row ST_AsGeoJSON calls. They are automatically refreshed on INSERT/UPDATE.
	BoundaryGeojson           string `gorm:"type:jsonb"` // Full-detail boundary Feature JSON
	BoundarySimplifiedGeojson string `gorm:"type:jsonb"` // Simplified boundary Feature JSON
}
