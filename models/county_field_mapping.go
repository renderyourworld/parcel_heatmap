package models

import (
	"database/sql"
)

// CountyFieldMapping holds config for mapping external field names to the columns.
type CountyFieldMapping struct {
	// ID maps to bigserial (uint64)
	ID           uint64         `gorm:"primaryKey;type:bigserial"`
	CountyID     uint16         `gorm:"not null;uniqueIndex:idx_county_source"` // FK to counties.id
	SourceField  string         `gorm:"type:varchar(50);not null;uniqueIndex:idx_county_source"`
	TargetColumn string         `gorm:"type:varchar(50);not null"`
	DataType     string         `gorm:"type:varchar(20)"`
	IsPIN        bool           `gorm:"default:false"`
	IsGeometry   bool           `gorm:"default:false"`
	Transform    sql.NullString `gorm:"type:text"`
}
