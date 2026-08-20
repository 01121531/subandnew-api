package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestManagedInstancePermissionRoutes(t *testing.T) {
	want := map[string]authz.Permission{
		http.MethodGet + " ":                                             authz.ManagedInstanceView,
		http.MethodGet + " /alerts":                                      authz.ManagedInstanceView,
		http.MethodPost + " /probe":                                      authz.ManagedInstanceCreate,
		http.MethodPost + " ":                                            authz.ManagedInstanceCreate,
		http.MethodPost + " /actions/batch/plan":                         authz.ManagedInstanceBatchOperate,
		http.MethodPost + " /actions/batch":                              authz.ManagedInstanceBatchOperate,
		http.MethodGet + " /actions/batch/:batch_id":                     authz.ManagedInstanceBatchOperate,
		http.MethodGet + " /realtime-events":                             authz.ManagedInstanceUsageView,
		http.MethodPost + " /realtime-refresh":                           authz.ManagedInstanceUsageView,
		http.MethodGet + " /dashboard-snapshots":                         authz.ManagedInstanceView,
		http.MethodPost + " /dashboard-refresh":                          authz.ManagedInstanceView,
		http.MethodGet + " /dashboard-events":                            authz.ManagedInstanceView,
		http.MethodGet + " /realtime-history":                            authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id":                                         authz.ManagedInstanceView,
		http.MethodGet + " /:id/inventory":                               authz.ManagedInstanceView,
		http.MethodGet + " /:id/metrics":                                 authz.ManagedInstanceView,
		http.MethodGet + " /:id/realtime-metrics":                        authz.ManagedInstanceView,
		http.MethodGet + " /:id/account-output":                          authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/conductor/key-usage":                     authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/account-management/snapshot":             authz.ManagedInstanceUsageView,
		http.MethodPost + " /:id/account-management/refresh":             authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/usage-records":                           authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/usage-records/filter-options":            authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/usage-records/summary":                   authz.ManagedInstanceUsageView,
		http.MethodPost + " /:id/usage-records/exports":                  authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/usage-records/exports/:task_id":          authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/usage-records/exports/:task_id/download": authz.ManagedInstanceUsageView,
		http.MethodGet + " /:id/usage-records/export":                    authz.ManagedInstanceUsageView,
		http.MethodPut + " /:id":                                         authz.ManagedInstanceUpdate,
		http.MethodDelete + " /:id":                                      authz.ManagedInstanceDelete,
		http.MethodPut + " /:id/credential":                              authz.ManagedInstanceSecretRotate,
		http.MethodPost + " /:id/check":                                  authz.ManagedInstanceOperate,
		http.MethodGet + " /:id/tasks/:task_id":                          authz.ManagedInstanceView,
		http.MethodGet + " /:id/audits":                                  authz.ManagedInstanceAudit,
		http.MethodGet + " /:id/alerts":                                  authz.ManagedInstanceView,
		http.MethodGet + " /:id/config":                                  authz.ManagedTemplateView,
		http.MethodPut + " /:id/config":                                  authz.ManagedTemplateApply,
		http.MethodPost + " /:id/config/refresh":                         authz.ManagedTemplateView,
		http.MethodPost + " /:id/config/apply/plan":                      authz.ManagedTemplateApply,
		http.MethodPost + " /:id/config/apply":                           authz.ManagedTemplateApply,
		http.MethodGet + " /:id/config/operations/:operation_id":         authz.ManagedTemplateApply,
		http.MethodPost + " /:id/actions/plan":                           authz.ManagedInstanceOperate,
		http.MethodPost + " /:id/actions":                                authz.ManagedInstanceOperate,
		http.MethodGet + " /:id/operations/:operation_id":                authz.ManagedInstanceOperate,
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
		"/api/uptime",
	}
	for _, route := range engine.Routes() {
		for _, prefix := range forbiddenPrefixes {
			assert.Falsef(t, strings.HasPrefix(route.Path, prefix), "%s %s must not be registered", route.Method, route.Path)
		}
	}
}

func TestManagedUsageExportRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/managed-usage-exports",
		"GET /api/managed-usage-exports/:task_id",
		"GET /api/managed-usage-exports/:task_id/download",
		"POST /api/managed-usage-exports/:task_id/cancel",
		"POST /api/managed-usage-exports/:task_id/retry",
		"DELETE /api/managed-usage-exports/:task_id",
	} {
		assert.Truef(t, routes[route], "%s must be registered", route)
	}
}
