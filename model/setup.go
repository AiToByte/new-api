package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
)

type Setup struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	Version       string `json:"version" gorm:"type:varchar(50);not null"`
	InitializedAt int64  `json:"initialized_at" gorm:"type:bigint;not null"`
}

func GetSetup() *Setup {
	var setup Setup
	err := DB.First(&setup).Error
	if err != nil {
		return nil
	}
	return &setup
}

// InitializeSetup records that system setup has completed, stamping the current
// version and timestamp. The record is written once by the setup wizard;
// callers never touch model.DB directly (Phase 3.2 layering).
func InitializeSetup() error {
	return DB.Create(&Setup{Version: common.Version, InitializedAt: time.Now().Unix()}).Error
}
