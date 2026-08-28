package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/binding"
	"github.com/01121531/subandnew-api/service/assistant/channel/wechatilink"
	"github.com/01121531/subandnew-api/service/assistant/channelservice"
	"github.com/01121531/subandnew-api/service/assistant/profile"
	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type assistantModelProfileCreateRequest struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKey          string `json:"api_key"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	Enabled         bool   `json:"enabled"`
	IsPrimary       bool   `json:"is_primary"`
}

type assistantModelProfileUpdateRequest struct {
	Name            *string `json:"name"`
	BaseURL         *string `json:"base_url"`
	Model           *string `json:"model"`
	APIKey          *string `json:"api_key"`
	TimeoutSeconds  *int    `json:"timeout_seconds"`
	MaxOutputTokens *int    `json:"max_output_tokens"`
	Enabled         *bool   `json:"enabled"`
	IsPrimary       *bool   `json:"is_primary"`
}

type assistantChannelLoginStatusRequest struct {
	VerifyCode string `json:"verify_code"`
}

type assistantBindingCodeRequest struct {
	Scope       string  `json:"scope"`
	InstanceIDs []int64 `json:"instance_ids"`
}

func CreateAssistantBindingCode(c *gin.Context) {
	var request assistantBindingCodeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_binding"})
		return
	}
	service, err := binding.NewService(model.DB)
	if err != nil {
		assistantError(c, err)
		return
	}
	code, err := service.CreateCode(c.Request.Context(), binding.CreateInput{
		UserID: c.GetInt("id"), CreatedBy: c.GetInt("id"), Scope: request.Scope, InstanceIDs: request.InstanceIDs,
	})
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": code})
}

func ListAssistantModelProfiles(c *gin.Context) {
	service, _ := profile.NewService(model.DB, nil)
	profiles, err := service.List()
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": profiles})
}

func CreateAssistantModelProfile(c *gin.Context) {
	var request assistantModelProfileCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_model_profile"})
		return
	}
	service, err := assistantProfileService(strings.TrimSpace(request.APIKey) != "")
	if err != nil {
		assistantError(c, err)
		return
	}
	created, err := service.Create(profile.CreateInput{
		Name: request.Name, Provider: request.Provider, BaseURL: request.BaseURL, Model: request.Model, APIKey: request.APIKey,
		TimeoutSeconds: request.TimeoutSeconds, MaxOutputTokens: request.MaxOutputTokens, Enabled: request.Enabled, IsPrimary: request.IsPrimary,
	})
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": created})
}

func UpdateAssistantModelProfile(c *gin.Context) {
	id, ok := assistantID(c, "profile_id")
	if !ok {
		return
	}
	var request assistantModelProfileUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_model_profile"})
		return
	}
	needsCipher := request.APIKey != nil && strings.TrimSpace(*request.APIKey) != ""
	service, err := assistantProfileService(needsCipher)
	if err != nil {
		assistantError(c, err)
		return
	}
	updated, err := service.Update(id, profile.UpdateInput{
		Name: request.Name, BaseURL: request.BaseURL, Model: request.Model, APIKey: request.APIKey,
		TimeoutSeconds: request.TimeoutSeconds, MaxOutputTokens: request.MaxOutputTokens,
		Enabled: request.Enabled, IsPrimary: request.IsPrimary,
	})
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": updated})
}

func DeleteAssistantModelProfile(c *gin.Context) {
	id, ok := assistantID(c, "profile_id")
	if !ok {
		return
	}
	service, _ := profile.NewService(model.DB, nil)
	if err := service.Delete(id); err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func TestAssistantModelProfile(c *gin.Context) {
	id, ok := assistantID(c, "profile_id")
	if !ok {
		return
	}
	service, err := assistantProfileService(false)
	if err != nil {
		assistantError(c, err)
		return
	}
	client, modelProfile, err := service.Client(id)
	if err != nil {
		assistantError(c, err)
		return
	}
	testContext, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	started := time.Now()
	response, err := client.Generate(testContext, provider.Request{
		Messages:        []provider.Message{{Role: provider.RoleSystem, Content: "This is a connectivity check. Reply only with OK."}, {Role: provider.RoleUser, Content: "OK"}},
		MaxOutputTokens: 8,
	})
	if err != nil || strings.TrimSpace(response.Message.Content) == "" {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "assistant_model_test_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"model": modelProfile.Model, "latency_ms": time.Since(started).Milliseconds(), "reachable": true,
	}})
}

func ListAssistantChannels(c *gin.Context) {
	service, err := assistantChannelService(false)
	if err != nil {
		assistantError(c, err)
		return
	}
	channels, err := service.List(c.Request.Context())
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": channels})
}

func StartAssistantChannelLogin(c *gin.Context) {
	service, err := assistantChannelService(true)
	if err != nil {
		assistantError(c, err)
		return
	}
	login, err := service.StartLogin(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": login})
}

func CheckAssistantChannelLogin(c *gin.Context) {
	id, ok := assistantID(c, "channel_id")
	if !ok {
		return
	}
	var request assistantChannelLoginStatusRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_channel_login"})
			return
		}
	}
	service, err := assistantChannelService(true)
	if err != nil {
		assistantError(c, err)
		return
	}
	login, err := service.CheckLogin(c.Request.Context(), id, request.VerifyCode)
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": login})
}

func CancelAssistantChannelLogin(c *gin.Context) {
	id, ok := assistantID(c, "channel_id")
	if !ok {
		return
	}
	service, err := assistantChannelService(false)
	if err == nil {
		err = service.CancelLogin(c.Request.Context(), id)
	}
	if err != nil {
		assistantError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type assistantRunListView struct {
	Items    []model.AssistantRun `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func ListAssistantRuns(c *gin.Context) {
	page, pageSize := assistantPagination(c)
	query := model.DB.WithContext(c.Request.Context()).Model(&model.AssistantRun{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		switch status {
		case model.AssistantRunStatusPending, model.AssistantRunStatusRunning, model.AssistantRunStatusSucceeded, model.AssistantRunStatusFailed, model.AssistantRunStatusCancelled:
			query = query.Where("status = ?", status)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_run_status"})
			return
		}
	}
	var total int64
	var runs []model.AssistantRun
	if err := query.Count(&total).Error; err != nil {
		assistantError(c, err)
		return
	}
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&runs).Error; err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": assistantRunListView{Items: runs, Total: total, Page: page, PageSize: pageSize}})
}

func GetAssistantRun(c *gin.Context) {
	publicID := strings.TrimSpace(c.Param("run_id"))
	if publicID == "" || len(publicID) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_id"})
		return
	}
	var run model.AssistantRun
	if err := model.DB.WithContext(c.Request.Context()).Where("run_id = ?", publicID).First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "assistant_resource_not_found"})
			return
		}
		assistantError(c, err)
		return
	}
	var calls []model.AssistantToolCall
	if err := model.DB.WithContext(c.Request.Context()).Where("run_id = ?", run.ID).Order("sequence ASC").Find(&calls).Error; err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"run": run, "tool_calls": calls}})
}

type assistantIdentityView struct {
	ID                   int64   `json:"id"`
	ChannelID            int64   `json:"channel_id"`
	ExternalUser         string  `json:"external_user"`
	UserID               int     `json:"user_id"`
	Username             string  `json:"username"`
	Status               string  `json:"status"`
	AllowedInstanceScope string  `json:"allowed_instance_scope"`
	InstanceIDs          []int64 `json:"instance_ids"`
	BoundAt              int64   `json:"bound_at"`
}

func ListAssistantIdentities(c *gin.Context) {
	var identities []model.AssistantIdentity
	if err := model.DB.WithContext(c.Request.Context()).Order("id DESC").Limit(500).Find(&identities).Error; err != nil {
		assistantError(c, err)
		return
	}
	views := make([]assistantIdentityView, 0, len(identities))
	for _, identity := range identities {
		var user model.User
		_ = model.DB.WithContext(c.Request.Context()).Select("id", "username").First(&user, identity.UserID).Error
		var instanceIDs []int64
		if identity.AllowedInstanceScope == model.AssistantInstanceScopeSelected {
			if err := model.DB.WithContext(c.Request.Context()).Model(&model.AssistantIdentityInstanceScope{}).Where("identity_id = ?", identity.ID).Order("instance_id ASC").Pluck("instance_id", &instanceIDs).Error; err != nil {
				assistantError(c, err)
				return
			}
		}
		views = append(views, assistantIdentityView{
			ID: identity.ID, ChannelID: identity.ChannelID, ExternalUser: maskAssistantExternalID(identity.ExternalUserID),
			UserID: identity.UserID, Username: user.Username, Status: identity.Status,
			AllowedInstanceScope: identity.AllowedInstanceScope, InstanceIDs: instanceIDs, BoundAt: identity.BoundAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": views})
}

func RevokeAssistantIdentity(c *gin.Context) {
	id, ok := assistantID(c, "identity_id")
	if !ok {
		return
	}
	err := model.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.AssistantIdentity{}).Where("id = ?", id).Update("status", model.AssistantIdentityStatusRevoked)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("identity_id = ?", id).Delete(&model.AssistantIdentityInstanceScope{}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "assistant_resource_not_found"})
		return
	}
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func RemoveAssistantChannelCredential(c *gin.Context) {
	id, ok := assistantID(c, "channel_id")
	if !ok {
		return
	}
	service, err := assistantChannelService(false)
	if err == nil {
		err = service.RemoveCredential(c.Request.Context(), id, c.GetInt("id"))
	}
	if err != nil {
		assistantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func assistantPagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func maskAssistantExternalID(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= 6 {
		return "***"
	}
	return string(runes[:3]) + "***" + string(runes[len(runes)-3:])
}

func assistantProfileService(needsCipher bool) (*profile.Service, error) {
	var cipher *secrets.Cipher
	if needsCipher {
		configured, err := secrets.NewFromEnvironment()
		if err != nil {
			return nil, err
		}
		cipher = configured
	} else if configured, err := secrets.NewFromEnvironment(); err == nil {
		cipher = configured
	}
	return profile.NewService(model.DB, cipher)
}

func assistantChannelService(needsCipher bool) (*channelservice.Service, error) {
	var cipher *secrets.Cipher
	configured, err := secrets.NewFromEnvironment()
	if err == nil {
		cipher = configured
	} else if needsCipher {
		return nil, err
	}
	return channelservice.NewService(model.DB, cipher, channelservice.Config{})
}

func assistantID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_id"})
		return 0, false
	}
	return id, true
}

func assistantError(c *gin.Context, err error) {
	switch wechatilink.KindOf(err) {
	case wechatilink.ErrorKindInvalid:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_channel_request"})
		return
	case wechatilink.ErrorKindCanceled:
		c.JSON(http.StatusRequestTimeout, gin.H{"success": false, "message": "assistant_channel_request_cancelled"})
		return
	case wechatilink.ErrorKindRateLimit:
		c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": "assistant_channel_rate_limited"})
		return
	case wechatilink.ErrorKindTimeout:
		c.JSON(http.StatusGatewayTimeout, gin.H{"success": false, "message": "assistant_channel_timeout"})
		return
	case wechatilink.ErrorKindAuthentication, wechatilink.ErrorKindSessionExpired:
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "assistant_channel_login_expired"})
		return
	case wechatilink.ErrorKindDNS, wechatilink.ErrorKindTLS, wechatilink.ErrorKindTCP,
		wechatilink.ErrorKindResponseTooLarge, wechatilink.ErrorKindHTTP,
		wechatilink.ErrorKindDecode, wechatilink.ErrorKindAPI:
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "assistant_channel_upstream_unavailable"})
		return
	}
	switch {
	case errors.Is(err, profile.ErrInvalidInput), errors.Is(err, binding.ErrInvalidBinding):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid_assistant_request"})
	case errors.Is(err, binding.ErrUserDenied), errors.Is(err, binding.ErrIdentityBound):
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "assistant_binding_denied"})
	case errors.Is(err, binding.ErrCodeInvalid):
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "assistant_binding_code_expired"})
	case errors.Is(err, profile.ErrNotFound), errors.Is(err, channelservice.ErrChannelNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "assistant_resource_not_found"})
	case errors.Is(err, profile.ErrSecretMissing), errors.Is(err, secrets.ErrKeyNotConfigured), errors.Is(err, channelservice.ErrChannelSecret):
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "assistant_secret_encryption_not_configured"})
	case errors.Is(err, channelservice.ErrLoginExpired):
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "assistant_channel_login_expired"})
	case errors.Is(err, channelservice.ErrLoginAlreadyComplete):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "assistant_channel_login_already_completed"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "assistant_operation_failed"})
	}
}
