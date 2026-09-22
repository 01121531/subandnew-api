package router

import (
	"github.com/01121531/subandnew-api/controller"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerDailyReportRoutes(api *gin.RouterGroup) {
	view := api.Group("/daily-reports")
	view.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.DailyReportView))
	view.GET("", controller.ListDailyReports)
	view.GET("/overview", controller.ListDailyReports)
	view.GET("/detail", controller.GetDailyReportDetail)
	view.GET("/accounts/overview", controller.ListDailyReportAccounts)
	view.GET("/accounts/detail", controller.ListDailyReportAccounts)
	view.GET("/uploads/overview", controller.ListDailyReportUploads)
	view.GET("/uploads/detail", controller.ListDailyReportUploads)
	view.GET("/suppliers/overview", controller.ListDailyReportSuppliers)
	view.GET("/suppliers/detail", controller.ListDailyReportSuppliers)
	view.POST("/export", middleware.RequirePermission(authz.DailyReportExport), controller.ExportDailyReport)
	view.POST("/accounts/export", middleware.RequirePermission(authz.DailyReportExport), controller.ExportDailyReportAccounts)
	view.POST("/suppliers/export", middleware.RequirePermission(authz.DailyReportExport), controller.ExportDailyReportSuppliers)
	view.POST("/uploads/export", middleware.RequirePermission(authz.DailyReportExport), controller.ExportDailyReportUploads)

	rules := api.Group("/daily-report-rules")
	rules.Use(middleware.AdminAuth())
	rules.GET("", middleware.RequirePermission(authz.DailyReportView), controller.ListDailyReportRules)
	rules.GET("/supplier-options", middleware.RequirePermission(authz.DailyReportView), controller.ListDailyReportSupplierOptions)
	rules.POST("", middleware.RequirePermission(authz.DailyReportManage), controller.SaveDailyReportRule)
	rules.POST("/from-filter-template", middleware.RequirePermission(authz.DailyReportManage), controller.PushDailyReportFilterTemplate)
	rules.PUT("/:id", middleware.RequirePermission(authz.DailyReportManage), controller.SaveDailyReportRule)
	rules.DELETE("/:id", middleware.RequirePermission(authz.DailyReportManage), controller.DeleteDailyReportRule)

	schedules := api.Group("/daily-report-schedules")
	schedules.Use(middleware.AdminAuth())
	schedules.GET("", middleware.RequirePermission(authz.DailyReportView), controller.ListDailyReportSchedules)
	schedules.POST("", middleware.RequirePermission(authz.DailyReportManage), controller.SaveDailyReportSchedule)
	schedules.PUT("/:id", middleware.RequirePermission(authz.DailyReportManage), controller.SaveDailyReportSchedule)
	schedules.DELETE("/:id", middleware.RequirePermission(authz.DailyReportManage), controller.DeleteDailyReportSchedule)
	schedules.POST("/:id/:action", middleware.RequirePermission(authz.DailyReportSend), controller.DailyReportScheduleAction)
	schedules.GET("/:id/runs", middleware.RequirePermission(authz.DailyReportView), controller.ListDailyReportScheduleRuns)
}
