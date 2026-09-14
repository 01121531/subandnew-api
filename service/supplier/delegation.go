package supplier

import (
	"context"
	"fmt"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
)

func (s *Service) ownerAccess(item *model.Supplier) (*authz.DataAccess, error) {
	id := item.ResponsibleAdminID
	if id == 0 {
		var root model.User
		if s.DB.Where("role >= ? AND status = ?", common.RoleRootUser, common.UserStatusEnabled).Order("id ASC").First(&root).Error != nil {
			return nil, fail(403, "supplier_owner_denied")
		}
		id = root.Id
	}
	a, err := authz.LoadDataAccess(s.DB, id)
	if err != nil || !a.Can(authz.SupplierManage) {
		return nil, fail(403, "supplier_owner_denied")
	}
	return a, nil
}

// BackfillLegacyOwners runs after users and Supplier have been migrated.
func BackfillLegacyOwners(db *gorm.DB) error {
	var count int64
	if err := db.Model(&model.Supplier{}).Where("responsible_admin_id = 0").Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	var root model.User
	if err := db.Where("role >= ? AND status = ?", common.RoleRootUser, common.UserStatusEnabled).Order("id ASC").First(&root).Error; err != nil {
		return err
	}
	return db.Model(&model.Supplier{}).Where("responsible_admin_id = 0").UpdateColumn("responsible_admin_id", root.Id).Error
}

func intersectPolicy(policy *model.SupplierEffectivePolicy, a *authz.DataAccess) {
	for key, granted := range policy.Values {
		field := key[strings.LastIndex(key, ".")+1:]
		switch field {
		case "group_name":
			field = "group"
		case "pool_rpm":
			field = "rpm"
		case "pool_concurrent":
			field = "concurrency"
		case "pool_available_accounts":
			field = "available_accounts"
		}
		allowed := true
		switch key {
		case "view_accounts":
			allowed = a.Can(authz.ManagedInstanceView)
		case "view_usage":
			allowed = a.Can(authz.ManagedInstanceUsageView)
		case "manage_proxies", "upload_accounts":
			allowed = a.AllFields() && a.Can(authz.ManagedInstanceOperate)
		default:
			allowed = a.CheckDataField(field) == nil
		}
		policy.Values[key] = granted && allowed
		if !allowed {
			policy.Sources[key] = "responsible_admin"
		}
	}
	policy.Version += fmt.Sprintf(":admin:%d:%d:%d", a.UserID, a.Version, a.Policy.Revision)
}

func (s *Service) CheckAdmin(actorID int, supplierID, bindingID int64) (*authz.DataAccess, error) {
	a, err := authz.LoadDataAccess(s.DB, actorID)
	if err != nil {
		return nil, fail(403, "supplier_permission_denied")
	}
	if supplierID == 0 {
		return a, nil
	}
	var item model.Supplier
	if s.DB.First(&item, supplierID).Error != nil {
		return nil, fail(404, "supplier_not_found")
	}
	if a.Role < common.RoleRootUser {
		if item.ResponsibleAdminID != actorID {
			return nil, fail(403, "supplier_permission_denied")
		}
		owner, err := s.ownerAccess(&item)
		if err != nil {
			return nil, err
		}
		var ids []int64
		q := s.DB.Model(&model.SupplierBinding{}).Where("supplier_id = ?", supplierID)
		if bindingID > 0 {
			q = q.Where("id = ?", bindingID)
		}
		if q.Pluck("instance_id", &ids).Error != nil || owner.CheckInstances(ids) != nil {
			return nil, fail(403, "supplier_permission_denied")
		}
	}
	return a, nil
}

func (s *Service) ListOwned(actorID, page, size int) ([]model.Supplier, int64, error) {
	a, err := s.CheckAdmin(actorID, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	q := s.DB.Model(&model.Supplier{})
	if a.Role < common.RoleRootUser {
		q = q.Where("responsible_admin_id = ?", actorID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []model.Supplier{}
	if err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	for i := range items {
		if err := s.decorateSupplier(&items[i]); err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func (s *Service) SaveOwned(ctx context.Context, actorID int, id int64, input SupplierInput) (*model.Supplier, error) {
	a, err := s.CheckAdmin(actorID, id, 0)
	if err != nil || !a.Can(authz.SupplierManage) {
		return nil, fail(403, "supplier_permission_denied")
	}
	var item *model.Supplier
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if a.Current(tx) != nil {
			return fail(403, "supplier_permission_denied")
		}
		local := New(tx)
		local.Now, local.RemoteFactory = s.Now, s.RemoteFactory
		var err error
		item, err = local.Save(id, input)
		if err != nil {
			return err
		}
		if id == 0 {
			item.ResponsibleAdminID = actorID
			return tx.Model(item).UpdateColumn("responsible_admin_id", actorID).Error
		}
		return nil
	})
	return item, err
}

func (s *Service) Takeover(ctx context.Context, id int64, actorID int, ip string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, err := authz.LoadDataAccess(tx, actorID)
		if err != nil || a.Role < common.RoleRootUser {
			return fail(403, "supplier_permission_denied")
		}
		var item model.Supplier
		if err := tx.First(&item, id).Error; err != nil {
			return err
		}
		previous := item.ResponsibleAdminID
		if err := tx.Model(&item).Updates(map[string]any{"responsible_admin_id": actorID, "auth_version": gorm.Expr("auth_version + 1")}).Error; err != nil {
			return err
		}
		if err := revoke(tx, id); err != nil {
			return err
		}
		return tx.Create(&model.SupplierAudit{SupplierID: id, AdminID: actorID, Action: "supplier.takeover", IPAddress: ip, StatusCode: 200, PolicyChanges: fmt.Sprintf("{\"previous_admin_id\":%d,\"responsible_admin_id\":%d}", previous, actorID)}).Error
	})
}

func (s *Service) CheckBindingGrant(actorID int, supplierID, bindingID, instanceID int64) error {
	a, err := s.CheckAdmin(actorID, supplierID, bindingID)
	if err != nil {
		return err
	}
	var item model.Supplier
	if s.DB.First(&item, supplierID).Error != nil {
		return fail(404, "supplier_not_found")
	}
	owner, err := s.ownerAccess(&item)
	if err != nil {
		return err
	}
	if instanceID == 0 && bindingID > 0 {
		var b model.SupplierBinding
		if s.DB.Where("supplier_id = ? AND id = ?", supplierID, bindingID).First(&b).Error != nil {
			return fail(404, "supplier_binding_not_found")
		}
		instanceID = b.InstanceID
	}
	if a.CheckInstances([]int64{instanceID}) != nil || owner.CheckInstances([]int64{instanceID}) != nil {
		return fail(403, "supplier_permission_denied")
	}
	return nil
}
