package service

import (
	"fmt"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func managedExportContractAccess(t *testing.T) *authz.DataAccess {
	t.Helper()
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AdminDataPolicy{}))
	user := model.User{Username: "export-rbac-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	for _, id := range []int64{1, 2} {
		require.NoError(t, model.DB.Create(&model.ManagedInstance{Id: id, Name: fmt.Sprintf("export-%d", id), Kind: model.ManagedInstanceKindGeneric, BaseURL: fmt.Sprintf("https://export-%d.example.test", id)}).Error)
	}
	policy := model.FullAdminDataPolicy()
	policy.UserID, policy.InstanceScope, policy.InstanceIDs = user.Id, "selected", []int64{1}
	require.NoError(t, model.DB.Create(&policy).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("user_id = ?", user.Id).Delete(&model.AdminDataPolicy{}).Error)
	})
	access, err := authz.LoadDataAccess(model.DB, user.Id)
	require.NoError(t, err)
	return access
}

func TestRBACContractExportDeniesCrossInstance(t *testing.T) {
	access := managedExportContractAccess(t)
	for _, kind := range []string{model.ManagedExportKindUsageRecords, model.ManagedExportKindAccounts} {
		t.Run(kind, func(t *testing.T) {
			record := &model.ManagedUsageExport{TaskID: "cross-" + kind, ExportKind: kind, InstanceID: 2, DataPolicy: &access.Policy}
			if kind == model.ManagedExportKindAccounts {
				record.InstanceID = 1
				for _, id := range []int64{1, 2} {
					require.NoError(t, model.DB.Create(&model.ManagedExportItem{TaskID: record.TaskID, InstanceID: id, ResourceID: id, Metadata: "{}"}).Error)
				}
			}
			for _, artifact := range []bool{false, true} {
				assert.ErrorIs(t, CheckManagedExportAccess(record, access, artifact), authz.ErrDataForbidden, "artifact=%t", artifact)
			}
		})
	}
	allowed := &model.ManagedUsageExport{TaskID: "allowed", ExportKind: model.ManagedExportKindAccounts, DataPolicy: &access.Policy}
	require.NoError(t, model.DB.Create(&model.ManagedExportItem{TaskID: allowed.TaskID, InstanceID: 1, ResourceID: 3, Metadata: "{}"}).Error)
	assert.NoError(t, CheckManagedExportAccess(allowed, access, true), "items belonging to another task must not affect scope")
}

func TestRBACContractExportArtifactAfterFieldRevocation(t *testing.T) {
	access := managedExportContractAccess(t)
	record := model.ManagedUsageExport{
		TaskID: "completed-export", InstanceID: 1, ActorID: access.UserID,
		ExportKind: model.ManagedExportKindUsageRecords, Status: model.ManagedUsageExportStatusSucceeded,
		DataPolicy: &access.Policy, FileName: "completed.csv",
	}
	require.NoError(t, model.DB.Create(&record).Error)
	stored, err := model.GetManagedUsageExport(record.TaskID)
	require.NoError(t, err)
	require.NotNil(t, stored.DataPolicy)
	require.NoError(t, CheckManagedExportAccess(stored, access, true))
	narrowed := model.FullAdminDataPolicy()
	narrowed.InstanceScope, narrowed.InstanceIDs, narrowed.Revision = "selected", []int64{1}, access.Policy.Revision
	narrowed.Fields["email"] = false
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error { return authz.SaveDataPolicy(tx, access.UserID, &narrowed, false) }))
	assert.ErrorIs(t, CheckManagedExportAccess(stored, access, true), authz.ErrAuthorizationChanged, "in-flight access must be invalidated")
	fresh, err := authz.LoadDataAccess(model.DB, access.UserID)
	require.NoError(t, err)
	assert.NoError(t, CheckManagedExportAccess(stored, fresh, false), "metadata is still scoped to an allowed instance")
	assert.ErrorIs(t, CheckManagedExportAccess(stored, fresh, true), authz.ErrDataForbidden, "a fresh session cannot download an artifact containing a revoked field")
	assert.True(t, stored.DataPolicy.Fields["email"], "current policy updates must not rewrite the artifact's historical field manifest")
}

func TestRBACContractExportScopeRevocationAndRoot(t *testing.T) {
	access := managedExportContractAccess(t)
	record := &model.ManagedUsageExport{TaskID: "scope-export", InstanceID: 1, ExportKind: model.ManagedExportKindUsageRecords, DataPolicy: &access.Policy}
	narrowed := model.EmptyAdminDataPolicy()
	narrowed.Fields, narrowed.Revision = model.FullAdminDataPolicy().Fields, access.Policy.Revision
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error { return authz.SaveDataPolicy(tx, access.UserID, &narrowed, false) }))
	assert.ErrorIs(t, CheckManagedExportAccess(record, access, true), authz.ErrAuthorizationChanged)
	fresh, err := authz.LoadDataAccess(model.DB, access.UserID)
	require.NoError(t, err)
	for _, artifact := range []bool{false, true} {
		assert.ErrorIs(t, CheckManagedExportAccess(record, fresh, artifact), authz.ErrDataForbidden, "selected-empty must deny metadata and artifact access")
	}
	root := model.User{Username: "export-rbac-root", Role: common.RoleRootUser}
	require.NoError(t, model.DB.Create(&root).Error)
	rootAccess, err := authz.LoadDataAccess(model.DB, root.Id)
	require.NoError(t, err)
	assert.NoError(t, CheckManagedExportAccess(record, rootAccess, true))
	require.NoError(t, model.DB.Model(&root).UpdateColumn("status", common.UserStatusDisabled).Error)
	assert.ErrorIs(t, CheckManagedExportAccess(record, rootAccess, true), authz.ErrAuthorizationChanged)
}

func TestRBACContractLegacyExportRequiresAllFields(t *testing.T) {
	access := managedExportContractAccess(t)
	legacy := &model.ManagedUsageExport{InstanceID: 1, ExportKind: model.ManagedExportKindUsageRecords}
	assert.NoError(t, CheckManagedExportAccess(legacy, access, true))
	narrowed := model.EmptyAdminDataPolicy()
	narrowed.InstanceIDs, narrowed.Revision = []int64{1}, access.Policy.Revision
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error { return authz.SaveDataPolicy(tx, access.UserID, &narrowed, false) }))
	fresh, err := authz.LoadDataAccess(model.DB, access.UserID)
	require.NoError(t, err)
	assert.NoError(t, CheckManagedExportAccess(legacy, fresh, false))
	assert.ErrorIs(t, CheckManagedExportAccess(legacy, fresh, true), authz.ErrDataForbidden, "legacy artifacts without a field manifest must be treated as full-data")
}
