package managedinstance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func operationAccessTestActor(t *testing.T, db *gorm.DB, ids ...int64) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	require.NoError(t, db.Create(&model.User{Id: 200, Username: "operation-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	policy := model.EmptyAdminDataPolicy()
	policy.UserID, policy.InstanceIDs = 200, ids
	require.NoError(t, db.Create(&policy).Error)
	require.NoError(t, authz.SetUserPermissions(200, authz.PermissionsMap{
		authz.ManagedInstanceOperate.Resource: {authz.ManagedInstanceOperate.Action: true, authz.ManagedInstanceBatchOperate.Action: true},
		authz.ManagedTemplateApply.Resource:   {authz.ManagedTemplateApply.Action: true},
	}))
}

func TestOperationActorAccessChecksCurrentFunctionAndAllInstances(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	operationAccessTestActor(t, db, 10)
	for _, action := range []string{model.ManagedInstanceActionToggleResource, model.ManagedInstanceActionApplyConfig} {
		_, err := LoadOperationActorAccess(200, action, false, 10)
		require.NoError(t, err)
		_, err = LoadOperationActorAccess(200, action, false, 10, 20)
		require.ErrorIs(t, err, authz.ErrDataForbidden)
	}
	// Change durable function grants without refreshing the local enforcer.
	require.NoError(t, db.Where("v0 = ?", authz.UserSubject(200)).Delete(&model.CasbinRule{}).Error)
	_, err := LoadOperationActorAccess(200, model.ManagedInstanceActionToggleResource, false, 10)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
	_, err = LoadOperationActorAccess(200, model.ManagedInstanceActionApplyConfig, false, 10)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
	_, err = LoadOperationActorAccess(0, model.ManagedInstanceActionToggleResource, false, 10)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
}

func TestRunOperationRechecksExecutingActorBeforeRemoteCalls(t *testing.T) {
	for _, revoke := range []string{"none", "function", "scope", "disabled", "deleted", "demoted", "batch-scope", "batch-function"} {
		t.Run(revoke, func(t *testing.T) {
			db := setupManagedInstanceOperationTestDB(t)
			instance := createOperationTestInstance(t, model.ManagedInstanceModeOperate, "channels.test")
			operationAccessTestActor(t, db, instance.Id, instance.Id+1)
			planned, err := PlanOperation(instance.Id, PlanOperationInput{
				Action: model.ManagedInstanceActionTestResources, IdempotencyKey: "access-worker-test",
				Parameters: json.RawMessage(`{"resource_ids":[1]}`), ActorID: 1,
			})
			require.NoError(t, err)
			queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
				OperationID: planned.OperationId, IdempotencyKey: "access-worker-test", ActorID: 200,
			})
			require.NoError(t, err)
			switch revoke {
			case "function":
				require.NoError(t, db.Where("v0 = ? AND v1 = ?", authz.UserSubject(200), authz.ManagedInstanceOperate.Resource).Delete(&model.CasbinRule{}).Error)
			case "scope":
				require.NoError(t, db.Model(&model.AdminDataPolicy{}).Where("user_id = ?", 200).Update("instance_ids", "[]").Error)
			case "disabled":
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", 200).Update("status", common.UserStatusDisabled).Error)
			case "deleted":
				require.NoError(t, db.Delete(&model.User{}, 200).Error)
			case "demoted":
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", 200).Update("role", common.RoleCommonUser).Error)
			case "batch-scope", "batch-function":
				require.NoError(t, db.Create(&[]model.ManagedInstanceOperationBatchItem{
					{BatchId: "access-batch", InstanceId: instance.Id, OperationId: planned.OperationId},
					{BatchId: "access-batch", InstanceId: instance.Id + 1, Position: 1},
				}).Error)
				if revoke == "batch-scope" {
					policy := model.EmptyAdminDataPolicy()
					policy.UserID, policy.InstanceIDs = 200, []int64{instance.Id}
					require.NoError(t, db.Save(&policy).Error)
				} else {
					require.NoError(t, db.Where("v0 = ? AND v1 = ? AND v2 = ?", authz.UserSubject(200), authz.ManagedInstanceBatchOperate.Resource, authz.ManagedInstanceBatchOperate.Action).Delete(&model.CasbinRule{}).Error)
				}
			}
			calls := 0
			previous := executeManagedInstanceRemoteOperation
			executeManagedInstanceRemoteOperation = func(ctx context.Context, _ *model.ManagedInstance, _ *CredentialMaterial, action string, _ json.RawMessage) (*remoteOperationResult, error) {
				calls++
				require.Equal(t, 200, authz.DataAccessFrom(ctx).UserID)
				return &remoteOperationResult{Action: action}, nil
			}
			t.Cleanup(func() { executeManagedInstanceRemoteOperation = previous })
			result, err := RunOperation(context.Background(), queued.OperationId, task.TaskID)
			if revoke == "none" {
				require.NoError(t, err)
				require.Equal(t, 1, calls)
			} else {
				require.Error(t, err)
				require.Zero(t, calls)
				require.Equal(t, "authorization_revoked", result.ErrorCode)
				require.Equal(t, model.ManagedInstanceOperationStatusFailed, result.Status)
			}
		})
	}
}

func TestOperationActorAccessDefaultDenyAndCollectorsUnaffected(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	require.NoError(t, db.Create(&model.User{Id: 201, Username: "new-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	_, err := LoadOperationActorAccess(201, model.ManagedInstanceActionToggleResource, false, 1)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
	require.NoError(t, authz.CheckContextInstances(context.Background(), 1, 2))
}
