package router

import (
	"github.com/01121531/subandnew-api/controller"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerSupplierRoutes(engine *gin.Engine, api *gin.RouterGroup) {
	admin := api.Group("/suppliers")
	admin.Use(controller.SupplierAuditTrail(), middleware.AdminAuth(), middleware.AnonymousRequestBodyLimit())
	view := middleware.RequirePermission(authz.SupplierView)
	manage := middleware.RequirePermission(authz.SupplierManage)
	admin.GET("", view, controller.ListSuppliers)
	admin.POST("", manage, controller.SaveSupplier)
	admin.GET("/instances", manage, controller.ListSupplierInstances)
	admin.GET("/:id", view, controller.GetSupplier)
	admin.PUT("/:id", manage, controller.SaveSupplier)
	admin.DELETE("/:id", manage, controller.DeleteSupplier)
	admin.POST("/:id/password", manage, controller.ResetSupplierPassword)
	admin.POST("/:id/revoke-sessions", manage, controller.RevokeSupplierSessions)
	admin.GET("/:id/bindings", view, controller.ListSupplierBindings)
	admin.POST("/:id/bindings", manage, controller.SaveSupplierBinding)
	admin.PUT("/:id/bindings/:binding_id", manage, controller.SaveSupplierBinding)
	admin.DELETE("/:id/bindings/:binding_id", manage, controller.DeleteSupplierBinding)
	admin.POST("/:id/bindings/:binding_id/test", manage, controller.TestSupplierBinding)
	admin.GET("/:id/audits", middleware.RequirePermission(authz.SupplierAudit), controller.ListSupplierAudits)

	portal := engine.Group("/supplier-api/v1")
	portal.Use(middleware.RouteTag("api"), middleware.GlobalAPIRateLimit(), middleware.AnonymousRequestBodyLimit(), controller.SupplierAuditTrail())
	portal.POST("/auth/login", controller.LoginSupplierPortal)
	portal.GET("/auth/session", controller.GetSupplierPortalSession)
	portal.POST("/auth/logout", controller.LogoutSupplierPortal)
	portal.POST("/auth/password", controller.ChangeSupplierPortalPassword)
	portal.GET("/bindings", controller.ListSupplierPortalBindings)
	portal.GET("/accounts", controller.ReadSupplierPortal("accounts"))
	portal.GET("/account-summary", controller.ReadSupplierPortal("account-summary"))
	portal.GET("/usage", controller.ReadSupplierPortal("usage"))
	portal.GET("/proxies", controller.ReadSupplierPortal("proxies"))
	portal.POST("/proxies", controller.WriteSupplierPortalProxy)
	portal.POST("/proxies/:id/test", controller.WriteSupplierPortalProxy)
	portal.PATCH("/proxies/:id", controller.WriteSupplierPortalProxy)
	portal.DELETE("/proxies/:id", controller.WriteSupplierPortalProxy)
	portal.GET("/account-upload/options", controller.ReadSupplierPortal("account-upload/options"))
	portal.POST("/account-upload/auth-url", controller.StartSupplierPortalUpload)
	portal.POST("/account-upload/exchange", controller.ExchangeSupplierPortalUpload)
}
