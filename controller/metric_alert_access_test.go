package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func alertHTTPTestDB(t *testing.T) *authz.DataAccess {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "alerts.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.AdminDataPolicy{}, &model.ManagedInstance{}, &model.ManagedInstanceAlert{},
		&model.ManagedInstanceAlertRule{}, &model.ManagedInstanceAlertRuleInstance{},
		&model.BillingAlertRule{}, &model.BillingAlertRuleInstance{}, &model.BillingAlertEvent{}, &model.BillingEmailDelivery{},
		&model.MetricAlertRule{}, &model.MetricAlertRuleInstance{}, &model.MetricAlertCondition{}, &model.MetricAlertState{},
	))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "alert-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	p := model.FullAdminDataPolicy()
	p.UserID, p.InstanceScope, p.InstanceIDs = 1, "selected", []int64{1}
	delete(p.Fields, "amount")
	delete(p.Fields, "email")
	require.NoError(t, db.Create(&p).Error)
	a, err := authz.LoadDataAccess(db, 1)
	require.NoError(t, err)
	return a
}

func alertHTTPRequest(t *testing.T, a *authz.DataAccess, method, route, url, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("id", a.UserID)
		c.Set("role", a.Role)
		c.Request = c.Request.WithContext(authz.WithDataAccess(c.Request.Context(), a))
	})
	r.Handle(method, route, handler)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(method, url, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, request)
	return w
}

func TestAlertHTTPRecordsScopeAndProjection(t *testing.T) {
	a := alertHTTPTestDB(t)
	require.NoError(t, model.DB.Create(&[]model.BillingAlertEvent{
		{EventKey: "visible", InstanceID: 1, SourceType: "billing", Threshold: "hidden-threshold", USDTotal: "hidden-cost", Recipients: "hidden-email", CreatedAt: 1},
		{EventKey: "other", InstanceID: 2, InstanceName: "hidden-instance", CreatedAt: 2},
	}).Error)
	w := alertHTTPRequest(t, a, "GET", "/records", "/records?page_size=1", "", ListBillingAlertRecords)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"total":1`)
	require.Contains(t, w.Body.String(), `"instance_id":1`)
	require.NotContains(t, w.Body.String(), "hidden")
	w = alertHTTPRequest(t, a, "GET", "/records", "/records?instance_id=2", "", ListBillingAlertRecords)
	require.Equal(t, http.StatusForbidden, w.Code)
	w = alertHTTPRequest(t, a, "GET", "/records/:id", "/records/2", "", GetBillingAlertRecord)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestAlertHTTPMixedBatchIsDeniedWithoutWrites(t *testing.T) {
	a := alertHTTPTestDB(t)
	for _, handler := range []gin.HandlerFunc{CreateBillingAlertRule, CreateMetricAlertRule, CreateInstanceAlertRule} {
		w := alertHTTPRequest(t, a, "POST", "/rules", "/rules", `{"instance_ids":[1,2]}`, handler)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	}
	for _, table := range []string{"billing_alert_rules", "metric_alert_rules", "managed_instance_alert_rules"} {
		var count int64
		require.NoError(t, model.DB.Table(table).Count(&count).Error)
		require.Zero(t, count)
	}
	w := alertHTTPRequest(t, a, "GET", "/capabilities", "/capabilities?instance_ids=1,2", "", ListMetricAlertCapabilities)
	require.Equal(t, http.StatusForbidden, w.Code)
	w = alertHTTPRequest(t, a, "GET", "/capabilities", "/capabilities?instance_ids=1,invalid", "", ListMetricAlertCapabilities)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAlertHTTPExistingMixedRuleCannotBeDeletedOrEvaluated(t *testing.T) {
	a := alertHTTPTestDB(t)
	require.NoError(t, model.DB.Create(&model.MetricAlertRule{ID: 1, Name: "mixed"}).Error)
	require.NoError(t, model.DB.Create(&[]model.MetricAlertRuleInstance{{RuleID: 1, InstanceID: 1}, {RuleID: 1, InstanceID: 2}}).Error)
	for _, handler := range []gin.HandlerFunc{DeleteMetricAlertRule, EvaluateMetricAlertRule} {
		w := alertHTTPRequest(t, a, "POST", "/rules/:id", "/rules/1", "", handler)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.MetricAlertRule{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestAlertHTTPProjectionAndFreshness(t *testing.T) {
	a := alertHTTPTestDB(t)
	handler := func(c *gin.Context) {
		metricAlertJSON(c, gin.H{"metric": "today_cost", "threshold": "hidden", "current_value": "hidden", "message": "hidden"}, nil)
	}
	w := alertHTTPRequest(t, a, "GET", "/projection", "/projection", "", handler)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "hidden")
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("authorization_version", a.Version+1).Error)
	w = alertHTTPRequest(t, a, "GET", "/projection", "/projection", "", handler)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
