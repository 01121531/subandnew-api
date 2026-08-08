package router

import (
	"net/http"
	"testing"

	"github.com/01121531/HUICHUAN-AI/service/authz"
	"github.com/stretchr/testify/assert"
)

func TestManagedInstancePermissionRoutes(t *testing.T) {
	want := map[string]authz.Permission{
		http.MethodGet + " ":                              authz.ManagedInstanceView,
		http.MethodGet + " /alerts":                       authz.ManagedInstanceView,
		http.MethodPost + " /probe":                       authz.ManagedInstanceCreate,
		http.MethodPost + " ":                             authz.ManagedInstanceCreate,
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
