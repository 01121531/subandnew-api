package router

import (
	"github.com/01121531/subandnew-api/controller"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerAssistantRoutes(api *gin.RouterGroup) {
	bindingCode := api.Group("/assistant/binding-code")
	bindingCode.Use(middleware.UserAuth(), middleware.RequirePermission(authz.AssistantAccess))
	bindingCode.POST("", controller.CreateAssistantBindingCode)

	assistantSelf := api.Group("/assistant/me")
	assistantSelf.Use(middleware.UserAuth(), middleware.RequirePermission(authz.AssistantAccess))
	assistantSelf.GET("/identities", controller.ListMyAssistantIdentities)
	assistantSelf.PUT("/identities/:identity_id/default-instance", controller.UpdateMyAssistantIdentityDefault)

	assistant := api.Group("/assistant")
	assistant.Use(middleware.AdminAuth())

	profiles := assistant.Group("/model-profiles")
	profiles.Use(middleware.RequirePermission(authz.AssistantManage))
	profiles.GET("", controller.ListAssistantModelProfiles)
	profiles.POST("", controller.CreateAssistantModelProfile)
	profiles.PUT("/:profile_id", controller.UpdateAssistantModelProfile)
	profiles.DELETE("/:profile_id", controller.DeleteAssistantModelProfile)
	profiles.POST("/:profile_id/test", controller.TestAssistantModelProfile)

	channels := assistant.Group("/channels")
	channels.Use(middleware.RequirePermission(authz.AssistantManage))
	channels.GET("", controller.ListAssistantChannels)
	channels.POST("/login", controller.StartAssistantChannelLogin)
	channels.POST("/:channel_id/login/status", controller.CheckAssistantChannelLogin)
	channels.DELETE("/:channel_id/login", controller.CancelAssistantChannelLogin)
	channels.DELETE("/:channel_id/credential", controller.RemoveAssistantChannelCredential)

	identities := assistant.Group("/identities")
	identities.Use(middleware.RequirePermission(authz.AssistantManage))
	identities.GET("", controller.ListAssistantIdentities)
	identities.PUT("/:identity_id/default-instance", controller.UpdateAssistantIdentityDefault)
	identities.DELETE("/:identity_id", controller.RevokeAssistantIdentity)

	settings := assistant.Group("/settings")
	settings.Use(middleware.RequirePermission(authz.AssistantManage))
	settings.GET("/default-instance", controller.GetAssistantDefaultInstanceSetting)
	settings.PUT("/default-instance", controller.UpdateAssistantDefaultInstanceSetting)
	assistant.GET("/instance-options", middleware.RequirePermission(authz.AssistantManage), controller.ListAssistantInstanceOptions)

	runs := assistant.Group("/runs")
	runs.Use(middleware.RequirePermission(authz.AssistantAudit))
	runs.GET("", controller.ListAssistantRuns)
	runs.GET("/:run_id", controller.GetAssistantRun)
}
