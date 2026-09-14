package model

import (
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SupplierPolicy map[string]*bool

var SupplierPolicyKeys = []string{
	"view_accounts", "view_usage", "manage_proxies", "upload_accounts",
	"account.email", "account.group_name", "account.status", "account.created_at",
	"account.total_cost", "account.today_cost", "account.total_requests", "account.total_tokens",
	"summary.total_accounts", "summary.available_accounts", "summary.rpm",
	"summary.pool_rpm", "summary.pool_concurrent", "summary.pool_available_accounts",
	"usage.cost", "usage.requests", "usage.tokens",
}

type SupplierPolicyDefault struct {
	PortalTitle    string          `json:"-" gorm:"type:varchar(256);not null;default:工作台"`
	UploadMethods  map[string]bool `json:"-" gorm:"serializer:json;type:text"`
	PortalRevision int64           `json:"-" gorm:"not null;default:1"`
	ID             int64           `json:"-" gorm:"primaryKey"`
	Policy         SupplierPolicy  `json:"policy" gorm:"serializer:json;type:text;not null"`
	Revision       int64           `json:"revision" gorm:"not null"`
}

func (SupplierPolicyDefault) TableName() string { return "supplier_policy_defaults" }

type SupplierEffectivePolicy struct {
	Values  map[string]bool   `json:"values"`
	Sources map[string]string `json:"sources"`
	Version string            `json:"version"`
}

func InitialSupplierPolicy() SupplierPolicy {
	result := SupplierPolicy{}
	for _, key := range SupplierPolicyKeys {
		enabled := key != "manage_proxies" && key != "upload_accounts"
		result[key] = &enabled
	}
	return result
}

func LegacySupplierPolicy(s Supplier) SupplierPolicy {
	result := InitialSupplierPolicy()
	result["view_accounts"] = &s.ViewAccounts
	result["view_usage"] = &s.ViewUsage
	result["manage_proxies"] = &s.ManageProxies
	result["upload_accounts"] = &s.UploadAccounts
	return result
}

func ResolveSupplierPolicy(defaults SupplierPolicyDefault, s Supplier, b *SupplierBinding) *SupplierEffectivePolicy {
	result := &SupplierEffectivePolicy{Values: map[string]bool{}, Sources: map[string]string{}, Version: fmt.Sprintf("%d:%d", defaults.Revision, s.PolicyVersion)}
	overrides := s.PolicyOverrides
	if overrides == nil {
		overrides = LegacySupplierPolicy(s)
	}
	for _, key := range SupplierPolicyKeys {
		result.Values[key] = defaults.Policy[key] != nil && *defaults.Policy[key]
		result.Sources[key] = "global"
		if value := overrides[key]; value != nil {
			result.Values[key] = *value
			result.Sources[key] = "supplier"
		}
		if b != nil {
			if value := b.PolicyOverrides[key]; value != nil {
				result.Values[key] = *value
				result.Sources[key] = "binding"
			}
		}
	}
	if b != nil {
		result.Version += fmt.Sprintf(":%d", b.PolicyVersion)
	}
	return result
}

// NULL marks pre-policy suppliers. Empty JSON objects mark deliberate inheritance.
func MigrateSupplierPolicies(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		defaults := SupplierPolicyDefault{ID: 1, Policy: InitialSupplierPolicy(), Revision: 1}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&defaults).Error; err != nil {
			return err
		}
		var after int64
		for {
			var items []Supplier
			if err := tx.Unscoped().Where("policy_overrides IS NULL AND id > ?", after).Order("id").Limit(500).Find(&items).Error; err != nil {
				return err
			}
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				item.PolicyOverrides = LegacySupplierPolicy(item)
				item.PolicyVersion = 1
				if err := tx.Unscoped().Model(&item).Where("policy_overrides IS NULL").Select("PolicyOverrides", "PolicyVersion").Updates(&item).Error; err != nil {
					return err
				}
				after = item.ID
			}
		}
		return nil
	})
}
