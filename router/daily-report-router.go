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
	view.POST("/export", middleware.RequirePermission(authz.DailyReportExport), controller.ExportDailyReport)

	rules := api.Group("/daily-report-rules")
	rules.Use(middleware.AdminAuth())
	rules.GET("", middleware.RequirePermission(authz.DailyReportView), controller.ListDailyReportRules)
	rules.POST("", middleware.RequirePermission(authz.DailyReportManage), controller.SaveDailyReportRule)
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
