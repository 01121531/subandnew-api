package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/01121531/HUICHUAN-AI/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestManagedInstancePermissionRoutes(t *testing.T) {
	want := map[string]authz.Permission{
		http.MethodGet + " ":                              authz.ManagedInstanceView,
		http.MethodGet + " /alerts":                       authz.ManagedInstanceView,
		http.MethodPost + " /probe":                       authz.ManagedInstanceCreate,
		http.MethodPost + " ":                             authz.ManagedInstanceCreate,
		http.MethodPost + " /actions/batch/plan":          authz.ManagedInstanceBatchOperate,
		http.MethodPost + " /actions/batch":               authz.ManagedInstanceBatchOperate,
		http.MethodGet + " /actions/batch/:batch_id":      authz.ManagedInstanceBatchOperate,
		http.MethodGet + " /:id":                          authz.ManagedInstanceView,
		http.MethodGet + " /:id/inventory":                authz.ManagedInstanceView,
		http.MethodGet + " /:id/metrics":                  authz.ManagedInstanceView,
		http.MethodPut + " /:id":                          authz.ManagedInstanceUpdate,
		http.MethodDelete + " /:id":                       authz.ManagedInstanceDelete,
		http.MethodPut + " /:id/credential":               authz.ManagedInstanceSecretRotate,
		http.MethodPost + " /:id/check":                   authz.ManagedInstanceOperate,
		http.MethodGet + " /:id/tasks/:task_id":           authz.ManagedInstanceView,
		http.MethodGet + " /:id/audits":                   authz.ManagedInstanceAudit,
		http.MethodGet + " /:id/alerts":                   authz.ManagedInstanceView,
		http.MethodPost + " /:id/actions/plan":            authz.ManagedInstanceOperate,
		http.MethodPost + " /:id/actions":                 authz.ManagedInstanceOperate,
		http.MethodGet + " /:id/operations/:operation_id": authz.ManagedInstanceOperate,
	}
	requireRoutes := make(map[string]authz.Permission, len(managedInstancePermissionRoutes))
	for _, route := range managedInstancePermissionRoutes {
		requireRoutes[route.method+" "+route.path] = route.permission
	}
	assert.Equal(t, want, requireRoutes)
}

func TestControlPlaneRouterExcludesLegacyBusinessRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	forbiddenPrefixes := []string{
		"/v1",
		"/api/channel",
		"/api/token",
		"/api/log",
		"/api/subscription",
		"/api/topup",
		"/api/redemption",
		"/api/deployments",
		"/api/task",
		"/api/mj",
		"/api/proxy",
		"/api/ratio",
	}
	for _, route := range engine.Routes() {
		for _, prefix := range forbiddenPrefixes {
			assert.Falsef(t, strings.HasPrefix(route.Path, prefix), "%s %s must not be registered", route.Method, route.Path)
		}
	}
}
