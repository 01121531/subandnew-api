package router

import (
	"net/http"

	"github.com/01121531/subandnew-api/controller"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
)

type permissionRoute struct {
	method     string
	path       string
	permission authz.Permission
	handler    gin.HandlerFunc
}

func registerManagedInstanceRoutes(apiRouter *gin.RouterGroup) {
	templateGroup := apiRouter.Group("/managed-config")
	templateGroup.Use(middleware.AdminAuth())
	templateGroup.GET("/schemas", middleware.RequirePermission(authz.ManagedTemplateView), controller.ListManagedConfigSchemas)
	templateGroup.GET("/templates", middleware.RequirePermission(authz.ManagedTemplateView), controller.ListManagedConfigTemplates)
	templateGroup.POST("/templates", middleware.RequirePermission(authz.ManagedTemplateApply), controller.CreateManagedConfigTemplate)
	templateGroup.PUT("/templates/:template_id", middleware.RequirePermission(authz.ManagedTemplateApply), controller.UpdateManagedConfigTemplate)
	templateGroup.DELETE("/templates/:template_id", middleware.RequirePermission(authz.ManagedTemplateApply), controller.DeleteManagedConfigTemplate)

	routeGroup := apiRouter.Group("/managed-instances")
	routeGroup.Use(middleware.AdminAuth())
	for _, route := range managedInstancePermissionRoutes {
		routeGroup.Handle(route.method, route.path, middleware.RequirePermission(route.permission), route.handler)
	}
}

var managedInstancePermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "", permission: authz.ManagedInstanceView, handler: controller.ListManagedInstances},
	{method: http.MethodGet, path: "/alerts", permission: authz.ManagedInstanceView, handler: controller.ListManagedInstanceAlerts},
	{method: http.MethodPost, path: "/probe", permission: authz.ManagedInstanceCreate, handler: controller.ProbeManagedInstance},
	{method: http.MethodPost, path: "", permission: authz.ManagedInstanceCreate, handler: controller.CreateManagedInstance},
	{method: http.MethodPost, path: "/actions/batch/plan", permission: authz.ManagedInstanceBatchOperate, handler: controller.PlanManagedInstanceBatchOperation},
	{method: http.MethodPost, path: "/actions/batch", permission: authz.ManagedInstanceBatchOperate, handler: controller.ExecuteManagedInstanceBatchOperation},
	{method: http.MethodGet, path: "/actions/batch/:batch_id", permission: authz.ManagedInstanceBatchOperate, handler: controller.GetManagedInstanceBatchOperation},
	{method: http.MethodGet, path: "/:id", permission: authz.ManagedInstanceView, handler: controller.GetManagedInstance},
	{method: http.MethodGet, path: "/:id/inventory", permission: authz.ManagedInstanceView, handler: controller.GetManagedInstanceInventory},
	{method: http.MethodGet, path: "/:id/metrics", permission: authz.ManagedInstanceView, handler: controller.GetManagedInstanceMetrics},
	{method: http.MethodGet, path: "/:id/usage-records", permission: authz.ManagedInstanceUsageView, handler: controller.GetManagedInstanceUsageRecords},
	{method: http.MethodGet, path: "/:id/usage-records/summary", permission: authz.ManagedInstanceUsageView, handler: controller.GetManagedInstanceUsageRecordSummary},
	{method: http.MethodPost, path: "/:id/usage-records/exports", permission: authz.ManagedInstanceUsageView, handler: controller.CreateManagedInstanceUsageRecordExport},
	{method: http.MethodGet, path: "/:id/usage-records/exports/:task_id", permission: authz.ManagedInstanceUsageView, handler: controller.GetManagedInstanceUsageRecordExport},
	{method: http.MethodGet, path: "/:id/usage-records/exports/:task_id/download", permission: authz.ManagedInstanceUsageView, handler: controller.DownloadManagedInstanceUsageRecordExport},
	{method: http.MethodGet, path: "/:id/usage-records/export", permission: authz.ManagedInstanceUsageView, handler: controller.ExportManagedInstanceUsageRecords},
	{method: http.MethodPut, path: "/:id", permission: authz.ManagedInstanceUpdate, handler: controller.UpdateManagedInstance},
	{method: http.MethodDelete, path: "/:id", permission: authz.ManagedInstanceDelete, handler: controller.DeleteManagedInstance},
	{method: http.MethodPut, path: "/:id/credential", permission: authz.ManagedInstanceSecretRotate, handler: controller.RotateManagedInstanceCredential},
	{method: http.MethodPost, path: "/:id/check", permission: authz.ManagedInstanceOperate, handler: controller.CheckManagedInstance},
	{method: http.MethodGet, path: "/:id/tasks/:task_id", permission: authz.ManagedInstanceView, handler: controller.GetManagedInstanceTask},
	{method: http.MethodGet, path: "/:id/audits", permission: authz.ManagedInstanceAudit, handler: controller.ListManagedInstanceAudits},
	{method: http.MethodGet, path: "/:id/alerts", permission: authz.ManagedInstanceView, handler: controller.ListManagedInstanceAlertsForInstance},
	{method: http.MethodGet, path: "/:id/config", permission: authz.ManagedTemplateView, handler: controller.GetManagedInstanceConfig},
	{method: http.MethodPut, path: "/:id/config", permission: authz.ManagedTemplateApply, handler: controller.SetManagedInstanceConfig},
	{method: http.MethodPost, path: "/:id/config/refresh", permission: authz.ManagedTemplateView, handler: controller.RefreshManagedInstanceConfig},
	{method: http.MethodPost, path: "/:id/config/apply/plan", permission: authz.ManagedTemplateApply, handler: controller.PlanManagedInstanceConfigApply},
	{method: http.MethodPost, path: "/:id/config/apply", permission: authz.ManagedTemplateApply, handler: controller.ExecuteManagedInstanceConfigApply},
	{method: http.MethodGet, path: "/:id/config/operations/:operation_id", permission: authz.ManagedTemplateApply, handler: controller.GetManagedInstanceConfigOperation},
	{method: http.MethodPost, path: "/:id/actions/plan", permission: authz.ManagedInstanceOperate, handler: controller.PlanManagedInstanceOperation},
	{method: http.MethodPost, path: "/:id/actions", permission: authz.ManagedInstanceOperate, handler: controller.ExecuteManagedInstanceOperation},
	{method: http.MethodGet, path: "/:id/operations/:operation_id", permission: authz.ManagedInstanceOperate, handler: controller.GetManagedInstanceOperation},
}
