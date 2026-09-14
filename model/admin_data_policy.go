package model

import (
	"errors"
	"slices"

	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var AdminDataFields = []string{"amount", "requests", "tokens", "rpm", "concurrency", "accounts", "rates", "email", "vendor", "group", "status", "time"}

type AdminDataPolicy struct {
	UserID        int             `json:"-" gorm:"primaryKey;autoIncrement:false"`
	InstanceScope string          `json:"instance_scope" gorm:"type:varchar(16);not null;default:'selected'"`
	InstanceIDs   []int64         `json:"instance_ids" gorm:"serializer:json;type:text"`
	Fields        map[string]bool `json:"fields" gorm:"serializer:json;type:text"`
	Revision      int64           `json:"revision" gorm:"not null;default:1"`
}

func EmptyAdminDataPolicy() AdminDataPolicy {
	return AdminDataPolicy{InstanceScope: "selected", InstanceIDs: []int64{}, Fields: map[string]bool{}}
}

func FullAdminDataPolicy() AdminDataPolicy {
	p := EmptyAdminDataPolicy()
	p.InstanceScope, p.Revision = "all", 1
	for _, key := range AdminDataFields {
		p.Fields[key] = true
	}
	return p
}

func (p AdminDataPolicy) Validate(tx *gorm.DB) error {
	if p.InstanceScope != "all" && p.InstanceScope != "selected" || p.Revision < 0 {
		return errors.New("invalid admin data policy")
	}
	if p.InstanceScope == "all" && len(p.InstanceIDs) != 0 {
		return errors.New("all-instance scope cannot include selected instances")
	}
	for key := range p.Fields {
		if !slices.Contains(AdminDataFields, key) {
			return errors.New("unknown admin data field")
		}
	}
	seen := map[int64]bool{}
	for _, id := range p.InstanceIDs {
		if id <= 0 || seen[id] {
			return errors.New("invalid or duplicate instance id")
		}
		seen[id] = true
	}
	if len(seen) > 0 {
		var count int64
		if err := tx.Model(&ManagedInstance{}).Where("id IN ?", p.InstanceIDs).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(seen)) {
			return errors.New("instance not found")
		}
	}
	return nil
}

func MigrateAdminDataPolicies(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var ids []int
		if err := tx.Model(&User{}).Where("role = ?", common.RoleAdminUser).
			Where("id NOT IN (?)", tx.Model(&AdminDataPolicy{}).Select("user_id")).Pluck("id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			policy := FullAdminDataPolicy()
			policy.UserID = id
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&policy).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
