package service

import (
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
)

func managedExportAccess(actorID int) (*authz.DataAccess, error) {
	a, err := authz.LoadDataAccess(model.DB, actorID)
	if err != nil {
		return nil, err
	}
	if !a.Can(authz.ManagedInstanceUsageView) {
		return nil, authz.ErrDataForbidden
	}
	return a, nil
}

func CheckManagedExportAccess(record *model.ManagedUsageExport, a *authz.DataAccess, artifact bool) error {
	if err := a.Current(model.DB); err != nil {
		return err
	}
	ids := []int64{}
	if record.InstanceID > 0 {
		ids = append(ids, record.InstanceID)
	}
	if record.ExportKind == model.ManagedExportKindAccounts {
		if err := model.DB.Model(&model.ManagedExportItem{}).Where("task_id = ?", record.TaskID).Distinct("instance_id").Pluck("instance_id", &ids).Error; err != nil {
			return err
		}
	}
	if len(ids) == 0 && a.Policy.InstanceScope != "all" {
		return authz.ErrDataForbidden
	}
	if err := a.CheckInstances(ids); err != nil {
		return err
	}
	if artifact {
		policy := model.FullAdminDataPolicy()
		if record.DataPolicy != nil {
			policy = *record.DataPolicy
		}
		for key, visible := range policy.Fields {
			if visible && !a.HasField(key) {
				return authz.ErrDataForbidden
			}
		}
	}
	return nil
}
