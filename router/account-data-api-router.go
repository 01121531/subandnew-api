package router

import (
	"github.com/01121531/subandnew-api/controller"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerAccountDataAPIRoutes(engine *gin.Engine, api *gin.RouterGroup) {
	management := api.Group("/account-data-apis")
	management.Use(middleware.AdminAuth())
	management.GET("", middleware.RequirePermission(authz.ManagedAccountAPIView), controller.ListAccountDataAPIs)
	management.GET("/instances", middleware.RequirePermission(authz.ManagedAccountAPIView), controller.ListAccountDataAPIInstanceOptions)
	management.GET("/:id", middleware.RequirePermission(authz.ManagedAccountAPIView), controller.GetAccountDataAPI)
	management.POST("", middleware.RequirePermission(authz.ManagedAccountAPIManage), controller.CreateAccountDataAPI)
	management.PUT("/:id", middleware.RequirePermission(authz.ManagedAccountAPIManage), controller.UpdateAccountDataAPI)
	management.DELETE("/:id", middleware.RequirePermission(authz.ManagedAccountAPIManage), controller.DeleteAccountDataAPI)
	management.POST("/preview", middleware.RequirePermission(authz.ManagedAccountAPIManage), controller.PreviewAccountDataAPI)
	management.POST("/:id/preview", middleware.RequirePermission(authz.ManagedAccountAPIManage), controller.PreviewAccountDataAPI)
	management.POST("/:id/keys", middleware.RequirePermission(authz.ManagedAccountAPIManage), controller.CreateAccountDataAPIKey)
	management.DELETE("/:id/keys/:key_id", middleware.RequirePermission(authz.ManagedAccountAPIManage), controller.RevokeAccountDataAPIKey)
	management.GET("/:id/access-logs", middleware.RequirePermission(authz.ManagedAccountAPIAudit), controller.ListAccountDataAPIAccessLogs)

	open := engine.Group("/open-api/v1")
	open.Use(middleware.RouteTag("api"))
	open.GET("/accounts", controller.GetOpenAccountData)
}
