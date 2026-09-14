package authz

import (
	"errors"
	"reflect"
	"slices"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func DenyAllPermissions() PermissionsMap {
	result := PermissionsMap{}
	for _, resource := range Catalog() {
		result[resource.Resource] = map[string]bool{}
		for _, action := range resource.Actions {
			result[resource.Resource][action.Action] = false
		}
	}
	return result
}

func SaveDataPolicy(tx *gorm.DB, userID int, input *model.AdminDataPolicy, create bool) error {
	if input == nil && !create {
		return nil
	}
	in := model.EmptyAdminDataPolicy()
	if input != nil {
		in = *input
	}
	if err := in.Validate(tx); err != nil {
		return err
	}
	in.UserID = userID
	in.InstanceIDs = slices.Clone(in.InstanceIDs)
	if in.InstanceIDs == nil {
		in.InstanceIDs = []int64{}
	}
	if in.Fields == nil {
		in.Fields = map[string]bool{}
	}
	slices.Sort(in.InstanceIDs)
	old := model.EmptyAdminDataPolicy()
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&old).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if create && err == nil {
		return ErrAuthorizationChanged
	}
	if !create && old.Revision != in.Revision {
		return ErrAuthorizationChanged
	}
	if err == nil && old.InstanceScope == in.InstanceScope && reflect.DeepEqual(old.InstanceIDs, in.InstanceIDs) && reflect.DeepEqual(old.Fields, in.Fields) {
		return nil
	}
	in.Revision = old.Revision + 1
	if err := tx.Save(&in).Error; err != nil {
		return err
	}
	return BumpAuthorizationVersion(tx, userID)
}

func BumpAuthorizationVersion(tx *gorm.DB, userID int) error {
	return tx.Model(&model.User{}).Where("id = ?", userID).
		UpdateColumn("authorization_version", gorm.Expr("authorization_version + 1")).Error
}

// Creation grants only the new instance, never additional actions or fields.
func GrantCreatedInstance(tx *gorm.DB, userID int, instanceID int64) error {
	var user model.User
	if err := tx.First(&user, userID).Error; err != nil {
		return err
	}
	if user.Role >= common.RoleRootUser {
		return nil
	}
	if user.Role != common.RoleAdminUser || user.Status != common.UserStatusEnabled {
		return ErrDataForbidden
	}
	var policy model.AdminDataPolicy
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&policy).Error; err != nil {
		return err
	}
	if policy.InstanceScope == "all" || slices.Contains(policy.InstanceIDs, instanceID) {
		return nil
	}
	policy.InstanceIDs = append(policy.InstanceIDs, instanceID)
	return SaveDataPolicy(tx, userID, &policy, false)
}
