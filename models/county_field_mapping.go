package models

import (
	"database/sql"
)

// CountyFieldMapping holds config for mapping external field names to the columns.
type CountyFieldMapping struct {
	// ID maps to bigserial (uint64)
	ID           uint64         `gorm:"primaryKey;type:bigserial"`
	CountyID     uint16         `gorm:"not null;uniqueIndex:idx_county_target"` // FK to counties.id
	SourceField  string         `gorm:"type:varchar(255);not null"`
	TargetColumn string         `gorm:"type:varchar(50);not null;uniqueIndex:idx_county_target"`
	Transform    sql.NullString `gorm:"type:text"`
}
