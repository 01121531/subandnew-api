package authz

import (
	"context"
	"errors"
	"slices"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

var ErrDataForbidden = errors.New("admin_data_forbidden")
var ErrAuthorizationChanged = errors.New("admin_authorization_changed")

type DataAccess struct {
	UserID  int
	Role    int
	Version int64
	Policy  model.AdminDataPolicy
}

type dataAccessContextKey struct{}

func WithDataAccess(ctx context.Context, access *DataAccess) context.Context {
	return context.WithValue(ctx, dataAccessContextKey{}, access)
}

func DataAccessFrom(ctx context.Context) *DataAccess {
	access, _ := ctx.Value(dataAccessContextKey{}).(*DataAccess)
	return access
}

func LoadDataAccess(db *gorm.DB, userID int) (*DataAccess, error) {
	var user model.User
	if err := db.First(&user, userID).Error; err != nil {
		return nil, ErrDataForbidden
	}
	if user.Status != common.UserStatusEnabled || user.Role < common.RoleAdminUser {
		return nil, ErrDataForbidden
	}
	a := &DataAccess{UserID: userID, Role: user.Role, Version: user.AuthorizationVersion, Policy: model.EmptyAdminDataPolicy()}
	if user.Role >= common.RoleRootUser {
		a.Policy = model.FullAdminDataPolicy()
		return a, nil
	}
	err := db.Where("user_id = ?", userID).First(&a.Policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return a, nil
	}
	return a, err
}

func (a *DataAccess) Current(db *gorm.DB) error {
	latest, err := LoadDataAccess(db, a.UserID)
	if err != nil || latest.Version != a.Version || latest.Role != a.Role || latest.Policy.Revision != a.Policy.Revision {
		return ErrAuthorizationChanged
	}
	return nil
}

func (a *DataAccess) HasField(field string) bool {
	return a.Role >= common.RoleRootUser || a.Policy.Fields[field]
}

func (a *DataAccess) HasInstance(id int64) bool {
	return id > 0 && (a.Policy.InstanceScope == "all" || slices.Contains(a.Policy.InstanceIDs, id))
}

func (a *DataAccess) CheckInstances(ids []int64) error {
	for _, id := range ids {
		if !a.HasInstance(id) {
			return ErrDataForbidden
		}
	}
	return nil
}

func (a *DataAccess) ScopeQuery(q *gorm.DB, column string) *gorm.DB {
	if a.Policy.InstanceScope == "all" {
		return q
	}
	if len(a.Policy.InstanceIDs) == 0 {
		return q.Where("1 = 0")
	}
	return q.Where(column+" IN ?", a.Policy.InstanceIDs)
}

func (a *DataAccess) AllFields() bool {
	for _, field := range model.AdminDataFields {
		if !a.HasField(field) {
			return false
		}
	}
	return true
}

func CheckContextInstances(ctx context.Context, ids ...int64) error {
	if a := DataAccessFrom(ctx); a != nil {
		if err := a.Current(model.DB); err != nil {
			return err
		}
		return a.CheckInstances(ids)
	}
	return nil // Scheduled collectors run without a user principal.
}
