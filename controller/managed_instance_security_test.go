package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRotateManagedInstanceCredentialRequiresRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/managed-instances/1/credential", strings.NewReader(`{"auth_type":"bearer_pat","secret":"test-secret"}`))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Params = gin.Params{{Key: "id", Value: "1"}}
	context.Set("id", 42)
	context.Set("role", common.RoleAdminUser)

	RotateManagedInstanceCredential(context)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "root access is required")
}

func TestManagedInstanceRealtimeIDs(t *testing.T) {
	ids, err := managedInstanceRealtimeIDs("3, 1,3,2")
	require.NoError(t, err)
	require.Equal(t, []int64{3, 1, 2}, ids)

	_, err = managedInstanceRealtimeIDs("0,1")
	require.Error(t, err)
	_, err = managedInstanceRealtimeIDs(strings.Repeat("1,", 101) + "2")
	require.NoError(t, err, "duplicate IDs do not count toward the 100 instance limit")

	unique := make([]string, 101)
	for index := range unique {
		unique[index] = strconv.Itoa(index + 1)
	}
	_, err = managedInstanceRealtimeIDs(strings.Join(unique, ","))
	require.Error(t, err)
}

func TestManagedInstanceRealtimeTopics(t *testing.T) {
	topics, err := managedInstanceRealtimeTopics("rpm,accounts,rpm")
	require.NoError(t, err)
	require.Len(t, topics, 2)
	require.Contains(t, topics, "rpm")
	require.Contains(t, topics, "accounts")

	_, err = managedInstanceRealtimeTopics("rpm,secrets")
	require.Error(t, err)
}
