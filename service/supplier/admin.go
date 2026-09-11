package supplier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SupplierInput struct {
	Name            string                `json:"name"`
	Username        string                `json:"username"`
	Password        string                `json:"password"`
	Enabled         *bool                 `json:"enabled"`
	ViewAccounts    *bool                 `json:"view_accounts"`
	ViewUsage       *bool                 `json:"view_usage"`
	ManageProxies   *bool                 `json:"manage_proxies"`
	UploadAccounts  *bool                 `json:"upload_accounts"`
	PolicyOverrides *model.SupplierPolicy `json:"policy_overrides"`
	PolicyRevision  string                `json:"policy_revision"`
}

func boolean(value *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	return fallback
}
func (s *Service) List(page, size int) ([]model.Supplier, int64, error) {
	items := []model.Supplier{}
	var total int64
	if err := s.DB.Model(&model.Supplier{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := s.DB.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error
	if err == nil {
		for i := range items {
			if err = s.decorateSupplier(&items[i]); err != nil {
				break
			}
		}
	}
	return items, total, err
}
func (s *Service) Save(id int64, in SupplierInput) (*model.Supplier, error) {
	if in.PolicyOverrides != nil {
		if err := validatePolicy(*in.PolicyOverrides, false); err != nil {
			return nil, err
		}
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Username = normalizeUsername(in.Username)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 96 || !usernamePattern.MatchString(in.Username) {
		return nil, fail(400, "supplier_invalid_identity")
	}
	var item model.Supplier
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		defaults, err := lockPolicy(tx)
		if err != nil {
			return err
		}
		if id != 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
				return fail(404, "supplier_not_found")
			}
		}
		var count int64
		if err := tx.Unscoped().Model(&model.Supplier{}).Where("username = ? AND id <> ?", in.Username, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fail(409, "supplier_username_conflict")
		}
		if id == 0 {
			if !validPassword(in.Password) {
				return fail(400, "supplier_invalid_password")
			}
			hash, err := common.Password2Hash(in.Password)
			if err != nil {
				return err
			}
			item.PasswordHash = hash
			item.Enabled = true
			item.ViewAccounts = true
			item.ViewUsage = true
			item.PolicyOverrides = model.SupplierPolicy{}
		} else if in.Password != "" {
			return fail(400, "supplier_use_password_reset")
		}
		item.Name = in.Name
		item.Username = in.Username
		item.Enabled = boolean(in.Enabled, item.Enabled)
		if in.PolicyRevision != "" && in.PolicyRevision != model.ResolveSupplierPolicy(defaults, item, nil).Version {
			return fail(409, "supplier_policy_changed")
		}
		before := item.PolicyOverrides
		if before == nil {
			before = model.LegacySupplierPolicy(item)
		}
		next := model.SupplierPolicy{}
		for key, value := range before {
			next[key] = value
		}
		if in.PolicyOverrides != nil {
			next = *in.PolicyOverrides
			if next == nil {
				next = model.SupplierPolicy{}
			}
		}
		for key, value := range map[string]*bool{"view_accounts": in.ViewAccounts, "view_usage": in.ViewUsage, "manage_proxies": in.ManageProxies, "upload_accounts": in.UploadAccounts} {
			if value != nil {
				next[key] = value
			}
		}
		item.PolicyChanges = policyChanges(before, next)
		item.PolicyOverrides = next
		item.PolicyVersion++
		effective := model.ResolveSupplierPolicy(defaults, item, nil).Values
		item.ViewAccounts, item.ViewUsage = effective["view_accounts"], effective["view_usage"]
		item.ManageProxies, item.UploadAccounts = effective["manage_proxies"], effective["upload_accounts"]
		item.AuthVersion++
		if err := tx.Save(&item).Error; err != nil {
			return conflict(err)
		}
		return revoke(tx, item.ID)
	})
	if err == nil {
		err = s.decorateSupplier(&item)
	}
	return &item, err
}
func conflict(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(text, "unique constraint") || strings.Contains(text, "duplicate entry") || strings.Contains(text, "duplicate key") {
		return fail(409, "supplier_binding_conflict")
	}
	return err
}
func revoke(tx *gorm.DB, id int64) error {
	if err := tx.Where("supplier_id = ?", id).Delete(&model.SupplierSession{}).Error; err != nil {
		return err
	}
	return tx.Where("supplier_id = ?", id).Delete(&model.SupplierOAuthFlow{}).Error
}
func (s *Service) Revoke(id int64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		r := tx.Model(&model.Supplier{}).Where("id = ?", id).UpdateColumn("auth_version", gorm.Expr("auth_version + 1"))
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return fail(404, "supplier_not_found")
		}
		return revoke(tx, id)
	})
}
func (s *Service) Delete(id int64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		r := tx.Delete(&model.Supplier{}, id)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return fail(404, "supplier_not_found")
		}
		if err := revoke(tx, id); err != nil {
			return err
		}
		return tx.Where("supplier_id = ?", id).Delete(&model.SupplierBinding{}).Error
	})
}
func (s *Service) Password(id int64, password, current string, self bool) error {
	if !validPassword(password) {
		return fail(400, "supplier_invalid_password")
	}
	item, err := s.Supplier(id)
	if err != nil {
		return err
	}
	if self && !common.ValidatePasswordAndHash(current, item.PasswordHash) {
		return fail(401, "supplier_current_password_incorrect")
	}
	hash, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		r := tx.Model(&model.Supplier{}).Where("id = ? AND auth_version = ?", id, item.AuthVersion).Updates(map[string]any{"password_hash": hash, "auth_version": item.AuthVersion + 1})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return fail(409, "supplier_changed")
		}
		return revoke(tx, id)
	})
}

type BindingInput struct {
	InstanceID      int64                 `json:"instance_id"`
	Identifier      string                `json:"identifier"`
	Password        string                `json:"password"`
	Enabled         *bool                 `json:"enabled"`
	PolicyOverrides *model.SupplierPolicy `json:"policy_overrides"`
	PolicyRevision  string                `json:"policy_revision"`
}

func (s *Service) Bindings(id int64, active bool) ([]model.SupplierBinding, error) {
	items := []model.SupplierBinding{}
	q := s.DB.Where("supplier_id = ?", id)
	if active {
		q = q.Where("enabled = ?", true)
	}
	if err := q.Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	result := make([]model.SupplierBinding, 0, len(items))
	for _, b := range items {
		if err := s.decorateBinding(&b); err != nil {
			return nil, err
		}
		var instance model.ManagedInstance
		if s.DB.First(&instance, b.InstanceID).Error != nil || instance.Kind != model.ManagedInstanceKindClaudeGateway {
			if active {
				continue
			}
			b.InstanceName = "Instance unavailable"
			result = append(result, b)
			continue
		}
		b.InstanceName = instance.Name
		if active {
			b.RemoteUsername = ""
		}
		result = append(result, b)
	}
	return result, nil
}
func (s *Service) SaveBinding(ctx context.Context, supplierID, id int64, in BindingInput) (*model.SupplierBinding, error) {
	if in.PolicyOverrides != nil {
		if err := validatePolicy(*in.PolicyOverrides, false); err != nil {
			return nil, err
		}
	}
	if id != 0 && in.PolicyOverrides != nil && in.Identifier == "" && in.Password == "" {
		return s.saveBindingPolicy(supplierID, id, in)
	}
	if _, err := s.Supplier(supplierID); err != nil {
		return nil, err
	}
	var instance model.ManagedInstance
	if in.InstanceID <= 0 || s.DB.First(&instance, in.InstanceID).Error != nil || instance.Kind != model.ManagedInstanceKindClaudeGateway {
		return nil, fail(400, "supplier_invalid_instance")
	}
	c, err := cipher()
	if err != nil {
		return nil, fail(503, "supplier_encryption_unavailable")
	}
	var b model.SupplierBinding
	oldRevision := ""
	if id != 0 {
		if s.DB.Where("id = ? AND supplier_id = ?", id, supplierID).First(&b).Error != nil {
			return nil, fail(404, "supplier_binding_not_found")
		}
		oldRevision = b.Revision
		if in.Identifier == "" && in.Password == "" && b.InstanceID == in.InstanceID {
			p, e := c.Decrypt(b.ID, "supplier-binding:v1", b.KeyVersion, b.Ciphertext)
			if e != nil {
				return nil, e
			}
			in.Identifier = p.UserID
			in.Password = p.Secret
		}
	}
	in.Identifier = strings.TrimSpace(in.Identifier)
	if in.Identifier == "" || len(in.Identifier) > 256 || in.Password == "" || len(in.Password) > 1024 || in.Password != strings.TrimSpace(in.Password) {
		return nil, fail(400, "supplier_invalid_upstream_credentials")
	}
	revision, err := token()
	if err != nil {
		return nil, err
	}
	remote, err := s.RemoteFactory(&instance, in.Identifier, in.Password, "supplier-verify:"+revision, "")
	if err != nil {
		return nil, err
	}
	identity, err := remote.Verify(ctx)
	if err != nil {
		return nil, err
	}
	if identity.ID == "" || len(identity.ID) > 128 {
		return nil, fail(502, "supplier_invalid_remote_identity")
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		defaults, err := lockPolicy(tx)
		if err != nil {
			return err
		}
		// Serialize scope changes on the supplier row, including SQLite writers.
		r := tx.Model(&model.Supplier{}).Where("id = ?", supplierID).UpdateColumn("updated_at", s.Now().Unix())
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return fail(404, "supplier_not_found")
		}
		if id == 0 {
			var n int64
			if err := tx.Model(&model.SupplierBinding{}).Where("supplier_id = ?", supplierID).Count(&n).Error; err != nil {
				return err
			}
			if n >= 20 {
				return fail(400, "supplier_binding_limit")
			}
		} else {
			var latest model.SupplierBinding
			if tx.Where("id = ? AND supplier_id = ? AND revision = ?", id, supplierID, oldRevision).First(&latest).Error != nil {
				return fail(409, "supplier_binding_changed")
			}
			if latest.PolicyVersion != b.PolicyVersion {
				return fail(409, "supplier_policy_changed")
			}
		}
		b.SupplierID = supplierID
		var owner model.Supplier
		if err := tx.First(&owner, supplierID).Error; err != nil {
			return err
		}
		if in.PolicyRevision != "" && in.PolicyRevision != model.ResolveSupplierPolicy(defaults, owner, &b).Version {
			return fail(409, "supplier_policy_changed")
		}
		before := b.PolicyOverrides
		if in.PolicyOverrides != nil {
			b.PolicyOverrides = *in.PolicyOverrides
		}
		if b.PolicyOverrides == nil {
			b.PolicyOverrides = model.SupplierPolicy{}
		}
		b.PolicyChanges = policyChanges(before, b.PolicyOverrides)
		b.PolicyVersion++
		b.InstanceID = in.InstanceID
		b.RemoteUserID = identity.ID
		b.RemoteUsername = identity.Username
		b.Enabled = boolean(in.Enabled, true)
		b.Revision = revision
		if id == 0 {
			if err := tx.Create(&b).Error; err != nil {
				return conflict(err)
			}
		}
		b.Ciphertext, b.KeyVersion, _, err = c.Encrypt(b.ID, "supplier-binding:v1", managedinstance.CredentialPayload{UserID: in.Identifier, Secret: in.Password})
		if err != nil {
			return err
		}
		if err := tx.Save(&b).Error; err != nil {
			return conflict(err)
		}
		if id != 0 {
			if err := tx.Model(&owner).UpdateColumn("auth_version", gorm.Expr("auth_version + 1")).Error; err != nil {
				return err
			}
			return revoke(tx, supplierID)
		}
		return nil
	})
	b.InstanceName = instance.Name
	if err == nil {
		err = s.decorateBinding(&b)
	}
	return &b, err
}
func (s *Service) DeleteBinding(supplierID, id int64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		r := tx.Where("id = ? AND supplier_id = ?", id, supplierID).Delete(&model.SupplierBinding{})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return fail(404, "supplier_binding_not_found")
		}
		return tx.Where("binding_id = ?", id).Delete(&model.SupplierOAuthFlow{}).Error
	})
}
func (s *Service) remoteFor(b *model.SupplierBinding) (Remote, error) {
	var instance model.ManagedInstance
	if s.DB.First(&instance, b.InstanceID).Error != nil || instance.Kind != model.ManagedInstanceKindClaudeGateway {
		return nil, fail(404, "supplier_invalid_instance")
	}
	c, err := cipher()
	if err != nil {
		return nil, fail(503, "supplier_encryption_unavailable")
	}
	p, err := c.Decrypt(b.ID, "supplier-binding:v1", b.KeyVersion, b.Ciphertext)
	if err != nil {
		return nil, fail(503, "supplier_encryption_unavailable")
	}
	return s.RemoteFactory(&instance, p.UserID, p.Secret, fmt.Sprintf("supplier:%d:%s:%d", b.ID, b.Revision, instance.UpdatedAt), b.RemoteUserID)
}
func (s *Service) TestBinding(ctx context.Context, supplierID, id int64) error {
	var b model.SupplierBinding
	if s.DB.Where("id = ? AND supplier_id = ?", id, supplierID).First(&b).Error != nil {
		return fail(404, "supplier_binding_not_found")
	}
	r, err := s.remoteFor(&b)
	if err != nil {
		return err
	}
	_, err = r.Verify(ctx)
	return err
}
