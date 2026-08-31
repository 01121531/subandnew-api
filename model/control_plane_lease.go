package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

type ControlPlaneLease struct {
	Name      string `json:"name" gorm:"type:varchar(128);primaryKey"`
	Holder    string `json:"holder" gorm:"type:varchar(255);not null;index"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint;not null;index"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;not null"`
}

func TryAcquireControlPlaneLease(name, holder string, now, expiresAt int64) (bool, error) {
	name = strings.TrimSpace(name)
	holder = strings.TrimSpace(holder)
	if name == "" || holder == "" || expiresAt <= now {
		return false, errors.New("invalid control plane lease")
	}
	result := DB.Model(&ControlPlaneLease{}).
		Where("name = ? AND (holder = ? OR expires_at <= ?)", name, holder, now).
		Updates(map[string]any{"holder": holder, "expires_at": expiresAt, "updated_at": now})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	lease := &ControlPlaneLease{Name: name, Holder: holder, ExpiresAt: expiresAt, UpdatedAt: now}
	if err := DB.Create(lease).Error; err == nil {
		return true, nil
	} else {
		var existing ControlPlaneLease
		lookupErr := DB.Select("name").Where("name = ?", name).First(&existing).Error
		if lookupErr == nil {
			return false, nil
		}
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return false, err
		}
		return false, lookupErr
	}
}

func RenewControlPlaneLease(name, holder string, now, expiresAt int64) (bool, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(holder) == "" || expiresAt <= now {
		return false, errors.New("invalid control plane lease")
	}
	result := DB.Model(&ControlPlaneLease{}).
		Where("name = ? AND holder = ?", name, holder).
		Updates(map[string]any{"expires_at": expiresAt, "updated_at": now})
	return result.RowsAffected > 0, result.Error
}

func ReleaseControlPlaneLease(name, holder string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(holder) == "" {
		return nil
	}
	return DB.Where("name = ? AND holder = ?", name, holder).Delete(&ControlPlaneLease{}).Error
}
