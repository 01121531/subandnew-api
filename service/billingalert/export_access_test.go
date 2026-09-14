package billingalert

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupExportPrincipal(t *testing.T, root bool) *authz.DataAccess {
	t.Helper()
	setupRepositoryTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = wasMaster })
	require.NoError(t, authz.Init(model.DB))
	role := common.RoleAdminUser
	if root {
		role = common.RoleRootUser
	}
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "export-owner", Role: role, Status: common.UserStatusEnabled}).Error)
	if !root {
		p := model.EmptyAdminDataPolicy()
		p.UserID, p.InstanceIDs = 1, []int64{1}
		p.Fields["rpm"] = true
		require.NoError(t, model.DB.Create(&p).Error)
	}
	a, err := LoadExportAccess(1)
	require.NoError(t, err)
	return a
}

func frozenExport(t *testing.T, a *authz.DataAccess, task string) *model.BillingAlertExport {
	t.Helper()
	record := &model.BillingAlertExport{TaskID: task, ActorID: a.UserID, Query: "{}", Status: "pending",
		DataPolicy: &a.Policy, AuthorizationVersion: a.Version, ActorRole: a.Role}
	require.NoError(t, model.DB.Create(record).Error)
	return record
}

func TestRootFrozenAlertExportKeepsPrincipalRole(t *testing.T) {
	a := setupExportPrincipal(t, true)
	t.Setenv("BILLING_ALERT_EXPORT_DIR", t.TempDir())
	record := frozenExport(t, a, "root-frozen")
	execution, err := executionExportAccess(record)
	require.NoError(t, err)
	require.Equal(t, common.RoleRootUser, execution.Role)
	require.NoError(t, execution.Current(model.DB))
	require.NoError(t, RunAlertExport(context.Background(), record.ID))
	require.NoError(t, model.DB.First(record, record.ID).Error)
	require.Equal(t, "succeeded", record.Status)
	_, err = GetAlertExport(record.TaskID, a.UserID, true)
	require.NoError(t, err)
}

func TestRestrictedCSVScopesRowsAndOmitsHiddenColumns(t *testing.T) {
	a := setupExportPrincipal(t, false)
	t.Setenv("BILLING_ALERT_EXPORT_DIR", t.TempDir())
	for _, event := range []*model.BillingAlertEvent{
		{EventKey: "rpm", SourceType: "metric", InstanceID: 1, InstanceName: "visible", MetricKey: "rpm", Threshold: "21", CreatedAt: 1},
		{EventKey: "hidden-instance", SourceType: "metric", InstanceID: 2, InstanceName: "hidden-instance", MetricKey: "rpm", Threshold: "secret", CreatedAt: 2},
		{EventKey: "hidden-aggregate", SourceType: "metric", InstanceID: 0, InstanceName: "hidden-aggregate", MetricKey: "rpm", Threshold: "secret", CreatedAt: 3},
		{EventKey: "billing", SourceType: "billing", InstanceID: 1, InstanceName: "visible", Threshold: "secret-threshold", USDTotal: "secret-cost", Recipients: "secret-email", Conditions: "secret-condition", ObservedValues: "secret-value", CreatedAt: 4},
	} {
		require.NoError(t, model.DB.Create(event).Error)
	}
	record := frozenExport(t, a, "restricted")
	require.NoError(t, RunAlertExport(context.Background(), record.ID))
	require.NoError(t, model.DB.First(record, record.ID).Error)
	require.EqualValues(t, 2, record.RecordCount)
	bytes, err := os.ReadFile(record.FilePath)
	require.NoError(t, err)
	require.NotContains(t, string(bytes), "secret")
	require.NotContains(t, string(bytes), "hidden")
	require.Contains(t, string(bytes), "21")
	file, err := os.Open(record.FilePath)
	require.NoError(t, err)
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Len(t, rows[0], 7)
}

func TestAlertExportChecksRevocationDuringExecution(t *testing.T) {
	a := setupExportPrincipal(t, true)
	directory := t.TempDir()
	t.Setenv("BILLING_ALERT_EXPORT_DIR", directory)
	record := frozenExport(t, a, "revoke-during-query")
	require.NoError(t, model.DB.Create(&model.BillingAlertEvent{EventKey: "one", InstanceID: 1}).Error)
	revoked := false
	callback := "test:revoke-export"
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if !revoked && tx.Statement.Table == "billing_alert_events" {
			revoked = true
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", a.UserID).UpdateColumn("authorization_version", a.Version+1).Error)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove(callback) })
	require.ErrorContains(t, RunAlertExport(context.Background(), record.ID), authz.ErrAuthorizationChanged.Error())
	require.True(t, revoked)
	require.NoError(t, model.DB.First(record, record.ID).Error)
	require.Equal(t, "failed", record.Status)
	files, err := filepath.Glob(filepath.Join(directory, "*"))
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestAlertExportDownloadRejectsScopeFieldAndActionRevocation(t *testing.T) {
	a := setupExportPrincipal(t, false)
	record := frozenExport(t, a, "download-revoke")
	require.NoError(t, CheckAlertExportAccess(record, a))
	record.DataPolicy = nil
	require.ErrorIs(t, CheckAlertExportAccess(record, a), authz.ErrDataForbidden)
	record.DataPolicy = &a.Policy
	require.NoError(t, model.DB.Model(&model.AdminDataPolicy{}).Where("user_id = ?", a.UserID).
		Updates(map[string]any{"instance_ids": "[]", "revision": a.Policy.Revision + 1}).Error)
	latest, err := LoadExportAccess(a.UserID)
	require.NoError(t, err)
	require.ErrorIs(t, CheckAlertExportAccess(record, latest), authz.ErrDataForbidden)
	require.NoError(t, model.DB.Model(&model.AdminDataPolicy{}).Where("user_id = ?", a.UserID).
		Updates(map[string]any{"instance_ids": "[1]", "fields": "{}", "revision": a.Policy.Revision + 2}).Error)
	latest, err = LoadExportAccess(a.UserID)
	require.NoError(t, err)
	require.ErrorIs(t, CheckAlertExportAccess(record, latest), authz.ErrDataForbidden)
	require.NoError(t, authz.SetUserPermissions(a.UserID, authz.PermissionsMap{authz.ResourceBillingAlert: {authz.BillingAlertActionView: false}}))
	_, err = LoadExportAccess(a.UserID)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
}

func TestAlertExportDisabledOwnerCannotExecute(t *testing.T) {
	a := setupExportPrincipal(t, true)
	record := frozenExport(t, a, "disabled")
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", a.UserID).Update("status", common.UserStatusDisabled).Error)
	require.ErrorContains(t, RunAlertExport(context.Background(), record.ID), authz.ErrDataForbidden.Error())
}

func TestAlertExportRefreshesCrossNodeActionRevocation(t *testing.T) {
	a := setupExportPrincipal(t, false)
	require.NoError(t, model.DB.Create(&model.CasbinRule{Ptype: "p", V0: authz.UserSubject(a.UserID),
		V1: authz.ResourceBillingAlert, V2: authz.BillingAlertActionView, V3: authz.EffectDeny}).Error)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", a.UserID).
		UpdateColumn("authorization_version", a.Version+1).Error)
	_, err := LoadExportAccess(a.UserID)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
}
