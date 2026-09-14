package controller

import (
	"bytes"
	"encoding/json"
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

func operationControllerAccessDB(t *testing.T) (*gorm.DB, *authz.DataAccess) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "operation-access.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.ManagedInstance{},
		&model.ManagedInstanceOperation{}, &model.ManagedInstanceOperationBatch{}, &model.ManagedInstanceOperationBatchItem{},
		&model.SystemTask{}, &model.ManagedInstanceAudit{}, &model.ManagedAccountFilterTemplate{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	require.NoError(t, db.Create(&model.User{Id: 101, Username: "scoped-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	policy := model.EmptyAdminDataPolicy()
	policy.UserID, policy.InstanceIDs = 101, []int64{1}
	require.NoError(t, db.Create(&policy).Error)
	access, err := authz.LoadDataAccess(db, 101)
	require.NoError(t, err)
	return db, access
}

func operationAccessRequest(access *authz.DataAccess, method, route, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("id", access.UserID)
		c.Set("role", access.Role)
		c.Request = c.Request.WithContext(authz.WithDataAccess(c.Request.Context(), access))
	})
	r.Handle(method, route, handler)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, request)
	return w
}

func TestBatchOperationScopeDenialHasNoPartialWrites(t *testing.T) {
	db, access := operationControllerAccessDB(t)
	response := operationAccessRequest(access, http.MethodPost, "/plan", "/plan",
		`{"action":"refresh_inventory","idempotency_key":"batch-scope-test","targets":[{"instance_id":1,"parameters":{}},{"instance_id":2,"parameters":{}}]}`, PlanManagedInstanceBatchOperation)
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	for _, table := range []any{&model.ManagedInstanceOperation{}, &model.ManagedInstanceOperationBatch{}, &model.ManagedInstanceOperationBatchItem{}, &model.SystemTask{}, &model.ManagedInstanceAudit{}} {
		var count int64
		require.NoError(t, db.Model(table).Count(&count).Error)
		require.Zero(t, count)
	}
	batch := &model.ManagedInstanceOperationBatch{BatchId: "scope-batch", ActorId: 101, Action: "refresh_inventory", Status: model.ManagedInstanceBatchStatusPlanned, TargetCount: 2}
	require.NoError(t, db.Create(batch).Error)
	require.NoError(t, db.Create(&[]model.ManagedInstanceOperationBatchItem{
		{BatchId: batch.BatchId, InstanceId: 1, Position: 0},
		{BatchId: batch.BatchId, InstanceId: 2, Position: 1},
	}).Error)
	for _, batchID := range []string{batch.BatchId, " \t" + batch.BatchId + " \n"} {
		body, err := json.Marshal(map[string]string{"batch_id": batchID, "idempotency_key": "batch-scope-test"})
		require.NoError(t, err)
		response = operationAccessRequest(access, http.MethodPost, "/execute", "/execute", string(body), ExecuteManagedInstanceBatchOperation)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	}
	response = operationAccessRequest(access, http.MethodGet, "/batch/:batch_id", "/batch/scope-batch", "", GetManagedInstanceBatchOperation)
	require.Equal(t, http.StatusForbidden, response.Code)
	var current model.ManagedInstanceOperationBatch
	require.NoError(t, db.First(&current, batch.Id).Error)
	require.Equal(t, model.ManagedInstanceBatchStatusPlanned, current.Status)
	require.Zero(t, current.ExecutedAt)
	for _, table := range []any{&model.ManagedInstanceOperation{}, &model.SystemTask{}, &model.ManagedInstanceAudit{}} {
		var count int64
		require.NoError(t, db.Model(table).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestOperationDetailProjectsFieldsAndRejectsOutOfScope(t *testing.T) {
	db, access := operationControllerAccessDB(t)
	operation := &model.ManagedInstanceOperation{OperationId: "projection-operation", InstanceId: 1, ActorId: 101,
		Action: model.ManagedInstanceActionTestResources, IdempotencyKey: "never-return-key", Parameters: `{"enabled":true}`,
		Plan: `{}`, Result: `{"count":3,"items":[{"email":"private@example.com","amount":12,"tokens":44}]}`}
	require.NoError(t, db.Create(operation).Error)
	response := operationAccessRequest(access, http.MethodGet, "/instances/:id/operations/:operation_id", "/instances/1/operations/projection-operation", "", GetManagedInstanceOperation)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	for _, hidden := range []string{"never-return-key", "private@example.com", `"amount"`, `"tokens"`, `"enabled"`, `"count"`} {
		require.NotContains(t, response.Body.String(), hidden)
	}
	require.Contains(t, response.Body.String(), "projection-operation")
	response = operationAccessRequest(access, http.MethodGet, "/instances/:id/operations/:operation_id", "/instances/2/operations/projection-operation", "", GetManagedInstanceOperation)
	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestOperationAndConfigEndpointsRejectScopeBeforeServiceWrites(t *testing.T) {
	db, access := operationControllerAccessDB(t)
	for _, handler := range []gin.HandlerFunc{PlanManagedInstanceOperation, ExecuteManagedInstanceOperation,
		GetManagedInstanceConfig, SetManagedInstanceConfig, RefreshManagedInstanceConfig, PlanManagedInstanceConfigApply,
		ExecuteManagedInstanceConfigApply, GetManagedInstanceConfigOperation} {
		response := operationAccessRequest(access, http.MethodPost, "/instances/:id", "/instances/2", `{}`, handler)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	}
	for _, table := range []any{&model.ManagedInstanceOperation{}, &model.SystemTask{}, &model.ManagedInstanceAudit{}} {
		var count int64
		require.NoError(t, db.Model(table).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestAccountFilterTemplateHiddenFieldsRejectedBeforeWrite(t *testing.T) {
	db, access := operationControllerAccessDB(t)
	template := &model.ManagedAccountFilterTemplate{ActorID: 101, Name: "original", MatchMode: "all", Rules: `[{"field":"email","operator":"is","values":["private@example.com"],"value_mode":"any"}]`}
	require.NoError(t, db.Create(template).Error)
	for _, field := range []string{"email", " email ", "vendor_name", "ownership", "group", "status", "amount", "tokens", "requests", "rpm", "active_sessions", "utilization_5h", "created_at", "unknown_field"} {
		body, err := json.Marshal(map[string]any{"name": "forbidden", "match_mode": "all", "rules": []any{map[string]any{"field": field, "operator": "is_empty", "value_mode": "any"}}})
		require.NoError(t, err)
		response := operationAccessRequest(access, http.MethodPost, "/templates", "/templates", string(body), CreateManagedAccountFilterTemplate)
		require.Equal(t, http.StatusForbidden, response.Code, field)
		response = operationAccessRequest(access, http.MethodPut, "/templates/:id", "/templates/1", string(body), UpdateManagedAccountFilterTemplate)
		require.Equal(t, http.StatusForbidden, response.Code, field)
	}
	var count int64
	require.NoError(t, db.Model(&model.ManagedAccountFilterTemplate{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
	var stored model.ManagedAccountFilterTemplate
	require.NoError(t, db.First(&stored, template.Id).Error)
	require.Equal(t, "original", stored.Name)
	response := operationAccessRequest(access, http.MethodGet, "/templates", "/templates", "", ListManagedAccountFilterTemplates)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "private@example.com")
	require.NotContains(t, response.Body.String(), "original")
}
