package service

import (
	"encoding/json"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"net/url"
	"strings"
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
	if record.ScheduleID > 0 {
		if err := CheckScheduledExportOwner(record, artifact); err != nil {
			return err
		}
	}
	return checkManagedExportAccess(record, a, artifact)
}

// Scheduled artifacts always retain their owner's restrictions, even when a
// superadministrator requests the file. This also covers queue progress checks.
func CheckScheduledExportOwner(record *model.ManagedUsageExport, artifact bool) error {
	a, err := managedExportAccess(record.ActorID)
	if err != nil {
		return err
	}
	var run model.ManagedAccountExportRun
	if err = model.DB.Where("task_id = ? AND schedule_id = ?", record.TaskID, record.ScheduleID).First(&run).Error; err != nil {
		return err
	}
	var frozen struct {
		Query struct {
			InstanceIDs []int64                             `json:"instance_ids"`
			SortBy      string                              `json:"sort_by"`
			Search      string                              `json:"search"`
			Include     []string                            `json:"include_terms"`
			Exclude     []string                            `json:"exclude_terms"`
			Rules       []managedinstance.AccountFilterRule `json:"rules"`
		} `json:"query"`
	}
	if err = json.Unmarshal([]byte(run.FrozenConfig), &frozen); err != nil {
		return err
	}
	if err = a.CheckInstances(frozen.Query.InstanceIDs); err != nil {
		return err
	}
	if err = a.CheckDataField(frozen.Query.SortBy); err != nil {
		return err
	}
	for _, rule := range frozen.Query.Rules {
		if err = a.CheckDataField(rule.Field); err != nil {
			return err
		}
	}
	if err = a.CheckQuery(url.Values{"search": {frozen.Query.Search}, "keyword": {strings.Join(append(frozen.Query.Include, frozen.Query.Exclude...), " ")}}); err != nil {
		return err
	}
	return checkManagedExportAccess(record, a, artifact)
}

func checkManagedExportAccess(record *model.ManagedUsageExport, a *authz.DataAccess, artifact bool) error {
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
