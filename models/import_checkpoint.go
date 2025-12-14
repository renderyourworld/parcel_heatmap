package models

import (
	"time"
)

// ImportCheckpoint tracks the progress of parcel data imports for resumability
type ImportCheckpoint struct {
	ID               uint       `gorm:"primaryKey;type:serial"`
	CountyName       string     `gorm:"type:varchar(50);not null;unique"`
	LastProcessedID  int64      `gorm:"type:bigint;default:0"`
	Status           string     `gorm:"type:varchar(20);default:'RUNNING'"`
	StartTime        *time.Time `gorm:"type:timestamp"`
	EndTime          *time.Time `gorm:"type:timestamp"`
	TotalProcessed   int       `gorm:"type:int;default:0"`
	TotalFailed      int       `gorm:"type:int;default:0"`
	CreatedAt        time.Time `gorm:"autoCreateTime"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime"`
}

func (ImportCheckpoint) TableName() string {
	return "import_checkpoints"
}
