package router

import (
	"github.com/01121531/subandnew-api/controller"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerBillingAlertRoutes(api *gin.RouterGroup) {
	alerts := api.Group("")
	alerts.Use(middleware.AdminAuth())
	alerts.GET("/alert-tasks", middleware.RequirePermission(authz.BillingAlertView), controller.ListAlertTasks)
	alerts.GET("/alert-metrics/capabilities", middleware.RequirePermission(authz.BillingAlertView), controller.ListMetricAlertCapabilities)
	alerts.GET("/metric-alert-rules", middleware.RequirePermission(authz.BillingAlertView), controller.ListMetricAlertRules)
	alerts.GET("/metric-alert-rules/:id", middleware.RequirePermission(authz.BillingAlertView), controller.GetMetricAlertRule)
	alerts.POST("/metric-alert-rules", middleware.RootAuth(), controller.CreateMetricAlertRule)
	alerts.PUT("/metric-alert-rules/:id", middleware.RootAuth(), controller.UpdateMetricAlertRule)
	alerts.DELETE("/metric-alert-rules/:id", middleware.RootAuth(), controller.DeleteMetricAlertRule)
	alerts.POST("/metric-alert-rules/:id/evaluate", middleware.RootAuth(), controller.EvaluateMetricAlertRule)

	billing := api.Group("/billing")
	billing.Use(middleware.AdminAuth())

	billing.GET("/filter-templates", middleware.RequirePermission(authz.BillingAlertView), controller.ListBillingFilterTemplates)
	billing.GET("/filter-templates/:id", middleware.RequirePermission(authz.BillingAlertView), controller.GetBillingFilterTemplate)
	billing.POST("/filter-templates", middleware.RootAuth(), controller.CreateBillingFilterTemplate)
	billing.POST("/filter-templates/:id/preview", middleware.RootAuth(), controller.PreviewBillingFilterTemplate)
	billing.PUT("/filter-templates/:id", middleware.RootAuth(), controller.UpdateBillingFilterTemplate)
	billing.DELETE("/filter-templates/:id", middleware.RootAuth(), controller.DeleteBillingFilterTemplate)

	billing.GET("/alert-rules", middleware.RequirePermission(authz.BillingAlertView), controller.ListBillingAlertRules)
	billing.GET("/alert-rules/:id", middleware.RequirePermission(authz.BillingAlertView), controller.GetBillingAlertRule)
	billing.POST("/alert-rules/preview", middleware.RootAuth(), controller.PreviewBillingAlertRule)
	billing.POST("/alert-rules", middleware.RootAuth(), controller.CreateBillingAlertRule)
	billing.POST("/alert-rules/:id/preview", middleware.RootAuth(), controller.PreviewBillingAlertRule)
	billing.PUT("/alert-rules/:id", middleware.RootAuth(), controller.UpdateBillingAlertRule)
	billing.DELETE("/alert-rules/:id", middleware.RootAuth(), controller.DeleteBillingAlertRule)
	billing.POST("/alert-rules/:id/evaluate", middleware.RootAuth(), controller.EvaluateBillingAlertRule)

	billing.GET("/alert-records", middleware.RequirePermission(authz.BillingAlertView), controller.ListBillingAlertRecords)
	billing.GET("/instance-alerts", middleware.RequirePermission(authz.BillingAlertView), controller.ListBillingInstanceAlerts)
	billing.GET("/alert-records/:id", middleware.RequirePermission(authz.BillingAlertView), controller.GetBillingAlertRecord)
	billing.POST("/alert-records/exports", middleware.RequirePermission(authz.BillingAlertView), controller.CreateBillingAlertRecordExport)
	billing.GET("/alert-record-exports", middleware.RequirePermission(authz.BillingAlertView), controller.ListBillingAlertRecordExports)
	billing.GET("/alert-record-exports/:task_id/download", middleware.RequirePermission(authz.BillingAlertView), controller.DownloadBillingAlertRecordExport)

	billing.GET("/exchange-rates", middleware.RequirePermission(authz.BillingAlertView), controller.ListBillingExchangeRates)
	billing.POST("/exchange-rates/refresh", middleware.RootAuth(), controller.RefreshBillingExchangeRate)
	billing.GET("/exchange-settings", middleware.RootAuth(), controller.GetBillingExchangeSettings)
	billing.PUT("/exchange-settings", middleware.RootAuth(), controller.UpdateBillingExchangeSettings)

	billing.GET("/smtp-settings", middleware.RootAuth(), controller.GetBillingSMTPSettings)
	billing.PUT("/smtp-settings", middleware.RootAuth(), controller.UpdateBillingSMTPSettings)
	billing.POST("/smtp-settings/test", middleware.RootAuth(), controller.TestBillingSMTPSettings)
}
