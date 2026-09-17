package accountexport

import (
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/stretchr/testify/require"
)

func TestPlanExecutionAndPauseAreAtomic(t *testing.T) {
	db, user, instance := setupSelectionTest(t, 3)
	require.NoError(t, db.AutoMigrate(&model.ManagedAccountExportSchedule{}, &model.ManagedAccountExportRun{}, &model.ManagedAccountExportDelivery{}, &model.ManagedUsageExport{}, &model.ManagedExportItem{}, &model.SMTPSetting{}))
	input := PlanInput{Name: "daily report", Config: Config{Scope: "dynamic", Schedule: Schedule{Kind: "daily", Hour: 9}, Period: ReportPeriod{Kind: "last30"}, Query: managedaccount.Query{InstanceIDs: []int64{instance.Id}, Dataset: "inventory"}, Recipients: []string{"one@example.test", "two@example.test"}}}
	view, err := SavePlan(user.Id, 0, input)
	require.NoError(t, err)
	plan := &view.ManagedAccountExportSchedule
	err = ChangePlanState(user.Id, plan.ID, plan.Version, "resume")
	require.ErrorIs(t, err, billingalert.ErrSMTPNotConfigured)
	require.NoError(t, db.Create(&model.SMTPSetting{ID: 1, Enabled: true, Host: "localhost", Port: 2525, FromAddress: "sender@example.test"}).Error)
	err = ChangePlanState(user.Id, plan.ID, plan.Version, "resume")
	require.NoError(t, err)
	plan, err = LoadPlan(plan.ID, user.Id)
	require.NoError(t, err)
	next := plan.NextAt
	run, err := ExecuteNow(t.Context(), user.Id, plan.ID, plan.Version)
	require.NoError(t, err)
	require.Equal(t, "exporting", run.Status)
	var deliveries []model.ManagedAccountExportDelivery
	require.NoError(t, db.Where("run_id = ?", run.ID).Find(&deliveries).Error)
	require.Len(t, deliveries, 2)
	require.NotEqual(t, deliveries[0].Recipient, deliveries[1].Recipient)
	var task model.ManagedUsageExport
	require.NoError(t, db.Where("task_id = ?", run.TaskID).First(&task).Error)
	require.Equal(t, plan.ID, task.ScheduleID)
	plan, err = LoadPlan(plan.ID, user.Id)
	require.NoError(t, err)
	require.Equal(t, next, plan.NextAt)
	skipped, err := ExecuteNow(t.Context(), user.Id, plan.ID, plan.Version)
	require.NoError(t, err)
	require.Equal(t, "skipped", skipped.Status)
	plan, err = LoadPlan(plan.ID, user.Id)
	require.NoError(t, err)
	err = ChangePlanState(user.Id, plan.ID, plan.Version-1, "pause")
	require.ErrorIs(t, err, model.ErrExportScheduleConflict)
	err = ChangePlanState(user.Id, plan.ID, plan.Version, "pause")
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&model.ManagedAccountExportDelivery{}).Where("run_id = ? AND status = ?", run.ID, "cancelled").Count(&count).Error)
	require.Equal(t, int64(2), count)
	require.NoError(t, db.Model(&user).Update("status", common.UserStatusDisabled).Error)
	require.Error(t, service.CheckScheduledExportOwner(&task, true), "old files must not be sent after owner revocation")
	_, err = LoadPlan(plan.ID, user.Id)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
}

func TestStartupSkipsMissedCyclesWithoutCreatingRuns(t *testing.T) {
	db, user, instance := setupSelectionTest(t, 1)
	require.NoError(t, db.AutoMigrate(&model.ManagedAccountExportSchedule{}, &model.ManagedAccountExportRun{}, &model.ManagedAccountExportDelivery{}, &model.SMTPSetting{}))
	view, err := SavePlan(user.Id, 0, PlanInput{Name: "interval", Config: Config{Scope: "dynamic", Schedule: Schedule{Kind: "interval", IntervalHours: 1, FirstAt: time.Now().Add(-24 * time.Hour).Unix()}, Period: ReportPeriod{Kind: "last30"}, Query: managedaccount.Query{InstanceIDs: []int64{instance.Id}, Dataset: "inventory"}, Recipients: []string{"one@example.test"}}})
	require.NoError(t, err)
	plan := &view.ManagedAccountExportSchedule
	require.NoError(t, db.Model(plan).Updates(map[string]any{"enabled": true, "next_at": time.Now().Add(-12 * time.Hour).Unix()}).Error)
	now := time.Now()
	require.NoError(t, skipMissed(now))
	require.NoError(t, db.First(plan, plan.ID).Error)
	require.Greater(t, plan.NextAt, now.Unix())
	require.LessOrEqual(t, plan.NextAt, now.Add(time.Hour).Unix())
	var count int64
	require.NoError(t, db.Model(&model.ManagedAccountExportRun{}).Count(&count).Error)
	require.Zero(t, count)
}
