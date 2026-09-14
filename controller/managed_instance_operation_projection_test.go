package controller

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManagedDTOHTTPPreservesActualFilterAndSchemaFields(t *testing.T) {
	_, access := operationControllerAccessDB(t)
	response := operationAccessRequest(access, http.MethodPost, "/templates", "/templates",
		`{"name":"safe-filter","match_mode":"all","rules":[{"field":"name","operator":"is","values":["public-account"],"value_mode":"any"}]}`,
		CreateManagedAccountFilterTemplate)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	for _, value := range []string{`"match_mode":"all"`, `"rules"`, `"operator":"is"`, `"value_mode":"any"`, `"values":["public-account"]`} {
		require.Contains(t, response.Body.String(), value)
	}
	response = operationAccessRequest(access, http.MethodGet, "/schemas", "/schemas", "", ListManagedConfigSchemas)
	require.Equal(t, http.StatusOK, response.Code)
	for _, key := range []string{"ui.site_name", "max_length", "remote_key", "enum"} {
		require.Contains(t, response.Body.String(), key)
	}
}

func TestManagedDTOHTTPTaskHookAndConfigProjectionCheckCurrentAccess(t *testing.T) {
	db, access := operationControllerAccessDB(t)
	preview := &managedinstance.ConfigPreview{
		Desired: map[string]any{"ui.site_name": "Public site", "email": "private-email"},
		Differences: []managedinstance.ConfigDiff{{Key: "email", Current: "private-before", Desired: "private-after"},
			{Key: "ui.site_name", Current: "Old site", Desired: "Public site"}}, ObservedHash: "private-hash",
	}
	configHandler := func(c *gin.Context) { adminManagedInstanceDTOJSON(c, http.StatusOK, preview) }
	response := operationAccessRequest(access, http.MethodGet, "/config", "/config", "", configHandler)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "private")
	require.Contains(t, response.Body.String(), `"current":"Old site"`)
	require.Contains(t, response.Body.String(), `"ui.site_name":"Public site"`)
	for _, payload := range []string{`"{\"email\":\"private-email\"}"`, `{"operation_id":"public-operation","secret":"private-secret"}`} {
		task := &model.SystemTask{TaskID: "public-task", Type: model.SystemTaskTypeManagedInstanceOperation,
			Status: model.SystemTaskStatusFailed, Payload: payload, Result: payload, State: payload, Error: "private-error"}
		response = operationAccessRequest(access, http.MethodGet, "/task", "/task", "", func(c *gin.Context) {
			adminManagedInstanceTaskJSON(c, http.StatusOK, task)
		})
		require.Equal(t, http.StatusOK, response.Code)
		require.NotContains(t, response.Body.String(), "private")
		require.Contains(t, response.Body.String(), `"status":"failed"`)
	}
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", access.UserID).Update("authorization_version", access.Version+1).Error)
	response = operationAccessRequest(access, http.MethodGet, "/config", "/config", "", configHandler)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.NotContains(t, response.Body.String(), "Public site")
}

func TestManagedDTOHTTPProjectionFailureNeverReturnsPartialData(t *testing.T) {
	_, access := operationControllerAccessDB(t)
	operation := &managedinstance.OperationView{ManagedInstanceOperation: &model.ManagedInstanceOperation{OperationId: "private-id"},
		Parameters: json.RawMessage(`invalid-json`)}
	response := operationAccessRequest(access, http.MethodGet, "/operation", "/operation", "", func(c *gin.Context) {
		adminManagedInstanceDTOJSON(c, http.StatusOK, operation)
	})
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.String(), "private")
	require.Contains(t, response.Body.String(), "data_projection_failed")
	_, err := managedinstance.ProjectManagedInstanceDTO(access, gin.H{"values": "private"})
	require.ErrorIs(t, err, authz.ErrDataForbidden)
}

func TestManagedProbeTaskEndpointProjectsAndRejectsMismatchedInstance(t *testing.T) {
	db, access := operationControllerAccessDB(t)
	task := &model.SystemTask{TaskID: "public-probe", Type: model.SystemTaskTypeManagedInstanceProbe,
		ScopeKey: "1", Status: model.SystemTaskStatusFailed, Payload: `{"instance_id":1,"email":"private-email"}`,
		Result: `{"password":"private-secret"}`, State: `{"amount":123}`, Error: "private-error"}
	require.NoError(t, db.Create(task).Error)
	route := "/instances/:id/tasks/:task_id"
	response := operationAccessRequest(access, http.MethodGet, route, "/instances/1/tasks/public-probe", "", GetManagedInstanceTask)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"status":"failed"`)
	require.NotContains(t, response.Body.String(), "private")
	require.NotContains(t, response.Body.String(), `"amount"`)
	response = operationAccessRequest(access, http.MethodGet, route, "/instances/2/tasks/public-probe", "", GetManagedInstanceTask)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.NoError(t, db.Model(task).Update("scope_key", "2").Error)
	response = operationAccessRequest(access, http.MethodGet, route, "/instances/1/tasks/public-probe", "", GetManagedInstanceTask)
	require.Equal(t, http.StatusNotFound, response.Code)
	require.NotContains(t, response.Body.String(), "public-probe")
}
