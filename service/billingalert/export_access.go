package billingalert

import (
	"encoding/json"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
)

func LoadExportAccess(actorID int) (*authz.DataAccess, error) {
	a, err := authz.LoadDataAccess(model.DB, actorID)
	if err != nil {
		return nil, err
	}
	if !a.Can(authz.BillingAlertView) {
		return nil, authz.ErrDataForbidden
	}
	return a, nil
}

// Old exports were unscoped and unprojected, so their artifacts require full access.
func CheckAlertExportAccess(record *model.BillingAlertExport, a *authz.DataAccess) error {
	if a == nil {
		return authz.ErrDataForbidden
	}
	if err := a.Current(model.DB); err != nil {
		return err
	}
	if !a.Can(authz.BillingAlertView) {
		return authz.ErrDataForbidden
	}
	if record.ActorID != a.UserID && a.Role < common.RoleRootUser {
		return authz.ErrDataForbidden
	}
	policy := model.FullAdminDataPolicy()
	if record.DataPolicy != nil {
		policy = *record.DataPolicy
	}
	var filter AlertRecordFilter
	if err := json.Unmarshal([]byte(record.Query), &filter); err != nil {
		return ErrInvalidBillingInput
	}
	if filter.InstanceID > 0 {
		if err := a.CheckInstances([]int64{filter.InstanceID}); err != nil {
			return err
		}
	} else if policy.InstanceScope == "all" {
		if a.Policy.InstanceScope != "all" {
			return authz.ErrDataForbidden
		}
	} else if err := a.CheckInstances(policy.InstanceIDs); err != nil {
		return err
	}
	for field, visible := range policy.Fields {
		if visible && !a.HasField(field) {
			return authz.ErrDataForbidden
		}
	}
	return ValidateRecordFilter(filter, a)
}

func executionExportAccess(record *model.BillingAlertExport) (*authz.DataAccess, error) {
	a, err := LoadExportAccess(record.ActorID)
	if err != nil {
		return nil, err
	}
	if record.DataPolicy != nil && (a.Version != record.AuthorizationVersion || a.Role != record.ActorRole || a.Policy.Revision != record.DataPolicy.Revision) {
		return nil, authz.ErrAuthorizationChanged
	}
	if err := CheckAlertExportAccess(record, a); err != nil {
		return nil, err
	}
	if record.DataPolicy != nil {
		// Never broaden a queued export when the live principal gains access.
		a.Policy = *record.DataPolicy
	}
	return a, nil
}
