package managedinstance

import (
	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
)

// LoadOperationActorAccess validates every explicit target before a caller queues
// work. Background operation workers must use the persisted executing actor.
func LoadOperationActorAccess(actorID int, action string, batch bool, instanceIDs ...int64) (*authz.DataAccess, error) {
	if actorID <= 0 {
		return nil, authz.ErrDataForbidden
	}
	access, err := authz.LoadDataAccess(model.DB, actorID)
	if err != nil {
		return nil, err
	}
	if err := access.CheckInstances(instanceIDs); err != nil {
		return nil, err
	}
	// Function grants may have changed on another node since this worker queued.
	if access.Role < common.RoleRootUser {
		if err := authz.ReloadPolicy(); err != nil {
			return nil, authz.ErrDataForbidden
		}
	}
	permission := authz.ManagedInstanceOperate
	if batch {
		permission = authz.ManagedInstanceBatchOperate
	}
	if action == model.ManagedInstanceActionApplyConfig {
		permission = authz.ManagedTemplateApply
	}
	if !authz.Can(actorID, access.Role, permission) {
		return nil, authz.ErrDataForbidden
	}
	if err := access.Current(model.DB); err != nil {
		return nil, err
	}
	return access, nil
}

func queuedOperationActorAccess(operation *model.ManagedInstanceOperation) (*authz.DataAccess, error) {
	// Resolve batch membership from durable records, not a caller-supplied payload.
	var batchIDs []string
	if err := model.DB.Model(&model.ManagedInstanceOperationBatchItem{}).
		Where("operation_id = ?", operation.OperationId).Distinct().Pluck("batch_id", &batchIDs).Error; err != nil {
		return nil, err
	}
	ids := []int64{operation.InstanceId}
	if len(batchIDs) > 0 {
		var batchInstanceIDs []int64
		if err := model.DB.Model(&model.ManagedInstanceOperationBatchItem{}).
			Where("batch_id IN ?", batchIDs).Pluck("instance_id", &batchInstanceIDs).Error; err != nil {
			return nil, err
		}
		ids = append(ids, batchInstanceIDs...)
	}
	return LoadOperationActorAccess(operation.ExecutedBy, operation.Action, len(batchIDs) > 0, ids...)
}
