package supplier

import (
	"encoding/json"
	"errors"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
)

func validatePolicy(policy model.SupplierPolicy, complete bool) error {
	allowed := map[string]bool{}
	for _, key := range model.SupplierPolicyKeys {
		allowed[key] = true
	}
	for key, value := range policy {
		if !allowed[key] || (complete && value == nil) {
			return fail(400, "supplier_invalid_policy")
		}
	}
	if complete && len(policy) != len(allowed) {
		return fail(400, "supplier_invalid_policy")
	}
	return nil
}

func defaultPolicy(db *gorm.DB) (model.SupplierPolicyDefault, error) {
	var defaults model.SupplierPolicyDefault
	err := db.First(&defaults, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SupplierPolicyDefault{ID: 1, Policy: model.InitialSupplierPolicy(), Revision: 1}, nil
	}
	return defaults, err
}

// All policy writers acquire the same database row before supplier/binding rows.
func lockPolicy(db *gorm.DB) (model.SupplierPolicyDefault, error) {
	initial := model.SupplierPolicyDefault{ID: 1, Policy: model.InitialSupplierPolicy(), Revision: 1}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
		return initial, err
	}
	if err := db.Model(&model.SupplierPolicyDefault{}).Where("id = ?", 1).UpdateColumn("revision", gorm.Expr("revision")).Error; err != nil {
		return initial, err
	}
	return defaultPolicy(db)
}

func (s *Service) DefaultPolicy() (model.SupplierPolicyDefault, error) { return defaultPolicy(s.DB) }

func (s *Service) SaveDefaultPolicy(policy model.SupplierPolicy, revision int64) (model.SupplierPolicyDefault, string, error) {
	var result model.SupplierPolicyDefault
	var changes string
	if err := validatePolicy(policy, true); err != nil {
		return result, changes, err
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		old, err := lockPolicy(tx)
		if err != nil {
			return err
		}
		if old.Revision != revision {
			return fail(409, "supplier_policy_changed")
		}
		result = old
		result.Policy, result.Revision = policy, old.Revision+1
		changes = policyChanges(old.Policy, policy)
		var suppliers []model.Supplier
		if err := tx.Find(&suppliers).Error; err != nil {
			return err
		}
		for _, item := range suppliers {
			affected := !reflect.DeepEqual(model.ResolveSupplierPolicy(old, item, nil).Values, model.ResolveSupplierPolicy(result, item, nil).Values)
			var bindings []model.SupplierBinding
			if err := tx.Where("supplier_id = ?", item.ID).Find(&bindings).Error; err != nil {
				return err
			}
			for _, binding := range bindings {
				if !reflect.DeepEqual(model.ResolveSupplierPolicy(old, item, &binding).Values, model.ResolveSupplierPolicy(result, item, &binding).Values) {
					affected = true
				}
			}
			if affected {
				if err := tx.Model(&item).UpdateColumn("auth_version", gorm.Expr("auth_version + 1")).Error; err != nil {
					return err
				}
				if err := revoke(tx, item.ID); err != nil {
					return err
				}
			}
		}
		return tx.Save(&result).Error
	})
	return result, changes, err
}

func policyChanges(before, after model.SupplierPolicy) string {
	changes := map[string]any{}
	for _, key := range model.SupplierPolicyKeys {
		if !reflect.DeepEqual(before[key], after[key]) {
			changes[key] = map[string]any{"before": before[key], "after": after[key]}
		}
	}
	if len(changes) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(changes)
	return string(encoded)
}

func (s *Service) decorateSupplier(item *model.Supplier) error {
	defaults, err := defaultPolicy(s.DB)
	if err != nil {
		return err
	}
	item.EffectivePolicy = model.ResolveSupplierPolicy(defaults, *item, nil)
	if a, err := s.ownerAccess(item); err == nil {
		intersectPolicy(item.EffectivePolicy, a)
	} else {
		for key := range item.EffectivePolicy.Values {
			item.EffectivePolicy.Values[key] = false
		}
	}
	item.EffectiveNaming = model.ResolveSupplierNaming(*item, nil)
	item.ViewAccounts = item.EffectivePolicy.Values["view_accounts"]
	item.ViewUsage = item.EffectivePolicy.Values["view_usage"]
	item.ManageProxies = item.EffectivePolicy.Values["manage_proxies"]
	item.UploadAccounts = item.EffectivePolicy.Values["upload_accounts"]
	return nil
}

func (s *Service) decorateBinding(b *model.SupplierBinding) error {
	var item model.Supplier
	if err := s.DB.First(&item, b.SupplierID).Error; err != nil {
		return err
	}
	defaults, err := defaultPolicy(s.DB)
	if err != nil {
		return err
	}
	b.EffectivePolicy = model.ResolveSupplierPolicy(defaults, item, b)
	b.EffectiveNaming = model.ResolveSupplierNaming(item, b)
	return nil
}

func (s *Service) saveBindingPolicy(supplierID, id int64, in BindingInput) (*model.SupplierBinding, error) {
	var b model.SupplierBinding
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		defaults, err := lockPolicy(tx)
		if err != nil {
			return err
		}
		var owner model.Supplier
		if err := tx.First(&owner, supplierID).Error; err != nil {
			return fail(404, "supplier_not_found")
		}
		if err := tx.Where("id = ? AND supplier_id = ?", id, supplierID).First(&b).Error; err != nil {
			return fail(404, "supplier_binding_not_found")
		}
		if in.InstanceID != 0 && in.InstanceID != b.InstanceID {
			return fail(400, "supplier_invalid_instance")
		}
		if in.PolicyRevision != "" && in.PolicyRevision != model.ResolveSupplierPolicy(defaults, owner, &b).Version {
			return fail(409, "supplier_policy_changed")
		}
		if err := updateBindingNaming(&b, owner, in); err != nil {
			return err
		}
		if in.DisplayName != nil {
			b.DisplayName = in.DisplayName
		}
		policyChanged := in.PolicyOverrides != nil && policyChanges(b.PolicyOverrides, *in.PolicyOverrides) != ""
		if policyChanged {
			b.PolicyChanges = policyChanges(b.PolicyOverrides, *in.PolicyOverrides)
			b.PolicyOverrides = *in.PolicyOverrides
			if b.PolicyOverrides == nil {
				b.PolicyOverrides = model.SupplierPolicy{}
			}
			b.PolicyVersion++
		}
		enabledChanged := in.Enabled != nil && *in.Enabled != b.Enabled
		b.Enabled = boolean(in.Enabled, b.Enabled)
		if err := tx.Save(&b).Error; err != nil {
			return err
		}
		if !policyChanged && !enabledChanged {
			if b.NamingChanges == "" {
				return nil
			}
			return tx.Where("binding_id = ? AND consumed_at = 0", b.ID).Delete(&model.SupplierOAuthFlow{}).Error
		}
		if err := tx.Model(&owner).UpdateColumn("auth_version", gorm.Expr("auth_version + 1")).Error; err != nil {
			return err
		}
		return revoke(tx, supplierID)
	})
	if err == nil {
		err = s.decorateBinding(&b)
	}
	if err == nil {
		var instance model.ManagedInstance
		if s.DB.First(&instance, b.InstanceID).Error == nil {
			b.InstanceName = instance.Name
		}
	}
	return &b, err
}
