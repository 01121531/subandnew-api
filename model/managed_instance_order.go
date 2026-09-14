package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Zero-valued legacy rows retain their original newest-first ordering.
const ManagedInstanceOrderSQL = "sort_order ASC, id DESC"

type ManagedInstanceOrderState struct {
	ID      int   `gorm:"primaryKey;autoIncrement:false"`
	Version int64 `gorm:"not null;default:1"`
}

func (ManagedInstanceOrderState) TableName() string { return "managed_instance_order_states" }

// LockManagedInstanceOrder must run inside a transaction. The write also locks
// SQLite, where SELECT FOR UPDATE is not supported.
func LockManagedInstanceOrder(tx *gorm.DB) (*ManagedInstanceOrderState, error) {
	state := ManagedInstanceOrderState{ID: 1, Version: 1}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&ManagedInstanceOrderState{}).Where("id = ?", 1).
		UpdateColumn("version", gorm.Expr("version")).Error; err != nil {
		return nil, err
	}
	if err := tx.First(&state, 1).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func AdvanceManagedInstanceOrder(tx *gorm.DB) error {
	return tx.Model(&ManagedInstanceOrderState{}).Where("id = ?", 1).
		UpdateColumn("version", gorm.Expr("version + 1")).Error
}
