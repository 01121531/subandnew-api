package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type managedInstanceCredentialRequest struct {
	AuthType  string `json:"auth_type"`
	Secret    string `json:"secret"`
	UserID    string `json:"user_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type managedInstanceRequest struct {
	Name                  string                            `json:"name"`
	Kind                  string                            `json:"kind"`
	BaseURL               string                            `json:"base_url"`
	Environment           string                            `json:"environment"`
	Labels                map[string]string                 `json:"labels"`
	ManagementMode        string                            `json:"management_mode"`
	TLSVerify             *bool                             `json:"tls_verify"`
	RequestTimeoutSeconds int                               `json:"request_timeout_seconds"`
	CheckIntervalSeconds  int                               `json:"check_interval_seconds"`
	Credential            *managedInstanceCredentialRequest `json:"credential"`
}

func ListManagedInstances(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := managedinstance.List(managedinstance.ListFilter{
		Kind: c.Query("kind"), Environment: c.Query("environment"), Status: c.Query("status"),
		Search: c.Query("search"), Page: page, PageSize: pageSize, SearchConnection: c.GetInt("role") >= common.RoleRootUser,
	})
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		for index, instance := range result.Items {
			result.Items[index] = managedinstance.RedactConnectionDetails(instance)
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func GetManagedInstance(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	instance, err := managedinstance.Get(id)
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		instance = managedinstance.RedactConnectionDetails(instance)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": instance})
}

func ProbeManagedInstance(c *gin.Context) {
	var request managedInstanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	result, err := managedinstance.ProbeConnection(c.Request.Context(), managedInstanceCreateInput(request, c.GetInt("id"), c.GetInt("role") >= common.RoleRootUser))
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func GetManagedInstanceInventory(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	result, err := managedinstance.CollectInventory(c.Request.Context(), id, c.DefaultQuery("resource", "auto"), c.Query("cursor"))
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func GetManagedInstanceMetrics(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	window := managedInstanceTimeWindow(c)
	result, err := managedinstance.CollectSummary(c.Request.Context(), id, window)
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func ListManagedInstanceAudits(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := managedinstance.ListAudits(id, page, pageSize)
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func ListManagedInstanceAlerts(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	instanceID, _ := strconv.ParseInt(c.Query("instance_id"), 10, 64)
	result, err := managedinstance.ListAlerts(managedinstance.AlertListFilter{
		InstanceID: instanceID, Status: c.Query("status"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func ListManagedInstanceAlertsForInstance(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := managedinstance.ListAlerts(managedinstance.AlertListFilter{
		InstanceID: id, Status: c.Query("status"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func GetManagedInstanceTask(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	task, err := model.GetSystemTaskByTaskID(c.Param("task_id"))
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	if task == nil || task.Type != model.SystemTaskTypeManagedInstanceProbe || task.ScopeKey != strconv.FormatInt(id, 10) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "managed instance task not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": task.ToResponse()})
}

func CreateManagedInstance(c *gin.Context) {
	var request managedInstanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	tlsVerify := true
	if request.TLSVerify != nil {
		tlsVerify = *request.TLSVerify
	}
	input := managedInstanceCreateInput(request, c.GetInt("id"), c.GetInt("role") >= common.RoleRootUser)
	input.TLSVerify = tlsVerify
	preflight, err := managedinstance.ProbeConnection(c.Request.Context(), input)
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	if !preflight.Success || preflight.Probe == nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"success": false, "message": preflight.ErrorCode, "data": preflight,
		})
		return
	}
	input.Preflight = preflight.Probe
	instance, err := managedinstance.Create(input)
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		instance = managedinstance.RedactConnectionDetails(instance)
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": instance})
}

func UpdateManagedInstance(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	var request managedInstanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	tlsVerify := true
	if request.TLSVerify != nil {
		tlsVerify = *request.TLSVerify
	}
	instance, err := managedinstance.Update(id, managedinstance.UpdateInput{
		Name: request.Name, Kind: request.Kind, BaseURL: request.BaseURL, Environment: request.Environment,
		Labels: request.Labels, ManagementMode: request.ManagementMode, TLSVerify: tlsVerify,
		RequestTimeoutSeconds: request.RequestTimeoutSeconds, CheckIntervalSeconds: request.CheckIntervalSeconds,
		ActorID:               c.GetInt("id"),
		AllowConnectionChange: c.GetInt("role") >= common.RoleRootUser,
		AllowWriteMode:        c.GetInt("role") >= common.RoleRootUser,
	})
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		instance = managedinstance.RedactConnectionDetails(instance)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": instance})
}

func RotateManagedInstanceCredential(c *gin.Context) {
	if c.GetInt("role") < common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "root access is required to rotate managed instance credentials"})
		return
	}
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	var request managedInstanceCredentialRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	credential, err := managedinstance.RotateCredential(id, *credentialInput(&request), c.GetInt("id"))
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": credential})
}

func CheckManagedInstance(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	task, _, err := service.EnqueueScopedSystemTask(
		model.SystemTaskTypeManagedInstanceProbe,
		strconv.FormatInt(id, 10),
		service.ManagedInstanceProbePayload{InstanceID: id, ActorID: c.GetInt("id")},
		nil,
	)
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "message": "", "data": task.ToResponse()})
}

func DeleteManagedInstance(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	if err := managedinstance.Delete(id, c.GetInt("id")); err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": id}})
}

func managedInstanceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid managed instance id"})
		return 0, false
	}
	return id, true
}

func credentialInput(request *managedInstanceCredentialRequest) *managedinstance.CredentialInput {
	if request == nil {
		return nil
	}
	return &managedinstance.CredentialInput{
		AuthType: request.AuthType, Secret: request.Secret, UserID: request.UserID, ExpiresAt: request.ExpiresAt,
	}
}

func managedInstanceCreateInput(request managedInstanceRequest, actorID int, allowWriteMode bool) managedinstance.CreateInput {
	tlsVerify := true
	if request.TLSVerify != nil {
		tlsVerify = *request.TLSVerify
	}
	return managedinstance.CreateInput{
		Name: request.Name, Kind: request.Kind, BaseURL: request.BaseURL, Environment: request.Environment,
		Labels: request.Labels, ManagementMode: request.ManagementMode, TLSVerify: tlsVerify,
		RequestTimeoutSeconds: request.RequestTimeoutSeconds, CheckIntervalSeconds: request.CheckIntervalSeconds,
		Credential: credentialInput(request.Credential), ActorID: actorID, AllowWriteMode: allowWriteMode,
	}
}

func managedInstanceTimeWindow(c *gin.Context) managedinstance.TimeWindow {
	start, _ := strconv.ParseInt(c.Query("start"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end"), 10, 64)
	return managedinstance.TimeWindow{Start: start, End: end}
}

func managedInstanceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, managedinstance.ErrInvalidInstance):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrInstanceNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": managedinstance.ErrInstanceNotFound.Error()})
	case errors.Is(err, managedinstance.ErrConnectionChangeForbidden):
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrWriteModeForbidden):
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrConfigOperationActive):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrCredentialKeyNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "managed instance credential encryption is not configured"})
	default:
		var probeError *managedinstance.ProbeError
		if errors.As(err, &probeError) {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": probeError.Code})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "managed instance operation failed"})
	}
}
