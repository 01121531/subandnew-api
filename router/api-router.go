package router

import (
	"github.com/01121531/HUICHUAN-AI/controller"
	"github.com/01121531/HUICHUAN-AI/middleware"

	// Providers register themselves for the optional administrator SSO flow.
	_ "github.com/01121531/HUICHUAN-AI/oauth"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// SetApiRouter exposes only control-plane APIs. Model relay, channel, token,
// billing, subscription, capture, playground, and public-registration routes
// intentionally do not belong to this process.
func SetApiRouter(router *gin.Engine) {
	api := router.Group("/api")
	api.Use(middleware.RouteTag("api"))
	api.Use(gzip.Gzip(gzip.DefaultCompression))
	api.Use(middleware.GlobalAPIRateLimit())
	requestBodyLimit := middleware.AnonymousRequestBodyLimit()

	api.GET("/setup", controller.GetSetup)
	api.POST("/setup", requestBodyLimit, controller.PostSetup)
	api.GET("/status", controller.GetStatus)
	api.GET("/uptime/status", controller.GetUptimeKumaStatus)
	api.GET("/status/test", middleware.AdminAuth(), controller.TestStatus)

	api.GET("/oauth/state", middleware.CriticalRateLimit(), controller.GenerateOAuthCode)
	api.GET("/oauth/:provider", middleware.CriticalRateLimit(), controller.HandleOAuth)
	api.POST("/verify", middleware.UserAuth(), middleware.CriticalRateLimit(), controller.UniversalVerify)

	registerControlPlaneUserRoutes(api, requestBodyLimit)
	registerControlPlaneOptionRoutes(api)
	registerControlPlaneOperationsRoutes(api)
	registerAuthzRoutes(api)
	registerManagedInstanceRoutes(api)
}

func registerControlPlaneUserRoutes(api *gin.RouterGroup, requestBodyLimit gin.HandlerFunc) {
	users := api.Group("/user")
	users.POST("/login", middleware.CriticalRateLimit(), requestBodyLimit, middleware.TurnstileCheck(), controller.Login)
	users.POST("/login/2fa", middleware.CriticalRateLimit(), requestBodyLimit, controller.Verify2FALogin)
	users.POST("/passkey/login/begin", middleware.CriticalRateLimit(), requestBodyLimit, controller.PasskeyLoginBegin)
	users.POST("/passkey/login/finish", middleware.CriticalRateLimit(), requestBodyLimit, controller.PasskeyLoginFinish)
	users.GET("/logout", controller.Logout)

	self := users.Group("/")
	self.Use(middleware.UserAuth())
	{
		self.GET("/self", controller.GetSelf)
		self.PUT("/self", middleware.CriticalRateLimit(), controller.UpdateSelf)
		self.GET("/passkey", controller.PasskeyStatus)
		self.POST("/passkey/register/begin", controller.PasskeyRegisterBegin)
		self.POST("/passkey/register/finish", controller.PasskeyRegisterFinish)
		self.POST("/passkey/verify/begin", controller.PasskeyVerifyBegin)
		self.POST("/passkey/verify/finish", controller.PasskeyVerifyFinish)
		self.DELETE("/passkey", controller.PasskeyDelete)
		self.GET("/2fa/status", controller.Get2FAStatus)
		self.POST("/2fa/setup", controller.Setup2FA)
		self.POST("/2fa/enable", controller.Enable2FA)
		self.POST("/2fa/disable", controller.Disable2FA)
		self.POST("/2fa/backup_codes", controller.RegenerateBackupCodes)
	}

	admin := users.Group("/")
	admin.Use(middleware.AdminAuth())
	{
		admin.GET("/", controller.GetAllUsers)
		admin.GET("/search", controller.SearchUsers)
		admin.GET("/:id", controller.GetUser)
		admin.POST("/", controller.CreateUser)
		admin.POST("/manage", controller.ManageUser)
		admin.PUT("/", controller.UpdateUser)
		admin.DELETE("/:id", controller.DeleteUser)
		admin.DELETE("/:id/reset_passkey", controller.AdminResetPasskey)
		admin.GET("/2fa/stats", controller.Admin2FAStats)
		admin.DELETE("/:id/2fa", controller.AdminDisable2FA)
	}
}

func registerControlPlaneOptionRoutes(api *gin.RouterGroup) {
	options := api.Group("/option")
	options.Use(middleware.RootAuth())
	{
		options.GET("/", controller.GetOptions)
		options.PUT("/", controller.UpdateOption)
	}

	providers := api.Group("/custom-oauth-provider")
	providers.Use(middleware.RootAuth())
	{
		providers.POST("/discovery", controller.FetchCustomOAuthDiscovery)
		providers.GET("/", controller.GetCustomOAuthProviders)
		providers.GET("/:id", controller.GetCustomOAuthProvider)
		providers.POST("/", controller.CreateCustomOAuthProvider)
		providers.PUT("/:id", controller.UpdateCustomOAuthProvider)
		providers.DELETE("/:id", controller.DeleteCustomOAuthProvider)
	}
}

func registerControlPlaneOperationsRoutes(api *gin.RouterGroup) {
	performance := api.Group("/performance")
	performance.Use(middleware.RootAuth())
	{
		performance.GET("/stats", controller.GetPerformanceStats)
		performance.POST("/gc", controller.ForceGC)
		performance.GET("/logs", controller.GetLogFiles)
		performance.DELETE("/logs", controller.CleanupLogFiles)
	}

	tasks := api.Group("/system-task")
	tasks.Use(middleware.RootAuth())
	{
		tasks.GET("/list", controller.ListSystemTasks)
		tasks.GET("/current", controller.GetCurrentSystemTask)
		tasks.GET("/:task_id", controller.GetSystemTask)
	}

	updates := api.Group("/system-update")
	updates.Use(middleware.RootAuth())
	{
		updates.GET("/capability", controller.GetSystemUpdateCapability)
		updates.GET("/latest", controller.GetLatestSystemUpdate)
		updates.GET("/status", controller.GetSystemUpdateStatus)
		updates.POST("", controller.StartSystemUpdate)
	}

	systemInfo := api.Group("/system-info")
	systemInfo.Use(middleware.RootAuth())
	{
		systemInfo.GET("/instances", controller.ListSystemInstances)
		systemInfo.DELETE("/stale-instances", controller.DeleteStaleSystemInstances)
		systemInfo.DELETE("/instances/:node_name", controller.DeleteStaleSystemInstance)
	}
}
