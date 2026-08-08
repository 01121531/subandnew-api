package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
)

type managedConfigTemplateRequest struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schema_version"`
	Values        json.RawMessage `json:"values"`
}

type managedConfigBindingRequest struct {
	TemplateID int64  `json:"template_id"`
	Mode       string `json:"mode"`
}

type managedConfigApplyPlanRequest struct {
	ExpectedObservedHash string `json:"expected_observed_hash"`
	IdempotencyKey       string `json:"idempotency_key"`
}

func ListManagedConfigSchemas(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": managedinstance.ListConfigSchemas()})
}

func ListManagedConfigTemplates(c *gin.Context) {
	result, err := managedinstance.ListConfigTemplates(c.Query("kind"))
	if err != nil {
		managedConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func CreateManagedConfigTemplate(c *gin.Context) {
	var request managedConfigTemplateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid config template request"})
		return
	}
	template, err := managedinstance.CreateConfigTemplate(managedConfigTemplateInput(request, c.GetInt("id")))
	if err != nil {
		managedConfigError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": template})
}

func UpdateManagedConfigTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("template_id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid config template id"})
		return
	}
	var request managedConfigTemplateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid config template request"})
		return
	}
	template, err := managedinstance.UpdateConfigTemplate(id, managedConfigTemplateInput(request, c.GetInt("id")))
	if err != nil {
		managedConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": template})
}

func DeleteManagedConfigTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("template_id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid config template id"})
		return
	}
	if err := managedinstance.DeleteConfigTemplate(id); err != nil {
		managedConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": id}})
}

func GetManagedInstanceConfig(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	binding, err := managedinstance.GetConfigBinding(id)
	if err != nil {
		if errors.Is(err, managedinstance.ErrConfigBindingNotFound) {
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": nil})
			return
		}
		managedConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": binding})
}

func SetManagedInstanceConfig(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	var request managedConfigBindingRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid config binding request"})
		return
	}
	binding, err := managedinstance.SetConfigBinding(id, managedinstance.ConfigBindingInput{
		TemplateID: request.TemplateID, Mode: request.Mode, ActorID: c.GetInt("id"),
	})
	if err != nil {
		managedConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": binding})
}

func RefreshManagedInstanceConfig(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	preview, err := managedinstance.RefreshConfigPreview(c.Request.Context(), id, c.GetInt("id"))
	if err != nil {
		managedConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": preview})
}

func PlanManagedInstanceConfigApply(c *gin.Context) {
	id, ok := managedInstanceID(c)
	if !ok {
		return
	}
	var request managedConfigApplyPlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid config apply plan request"})
		return
	}
	operation, err := managedinstance.PlanConfigApply(c.Request.Context(), id, managedinstance.PlanConfigApplyInput{
		ExpectedObservedHash: request.ExpectedObservedHash, IdempotencyKey: request.IdempotencyKey, ActorID: c.GetInt("id"),
	})
	if err != nil {
		managedConfigError(c, err)
		return
	}
	status := http.StatusCreated
	if operation.IdempotentReplay {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"success": true, "message": "", "data": operation})
}

func ExecuteManagedInstanceConfigApply(c *gin.Context) {
	executeManagedInstanceOperation(c, "apply_config", "")
}

func GetManagedInstanceConfigOperation(c *gin.Context) {
	getManagedInstanceOperation(c, "apply_config", "")
}

func managedConfigTemplateInput(request managedConfigTemplateRequest, actorID int) managedinstance.ConfigTemplateInput {
	return managedinstance.ConfigTemplateInput{
		Name: request.Name, Description: request.Description, Kind: request.Kind,
		SchemaVersion: request.SchemaVersion, Values: request.Values, ActorID: actorID,
	}
}

func managedConfigError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, managedinstance.ErrInvalidConfigTemplate), errors.Is(err, managedinstance.ErrInvalidOperation):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrConfigTemplateNotFound), errors.Is(err, managedinstance.ErrConfigBindingNotFound), errors.Is(err, managedinstance.ErrInstanceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrConfigTemplateInUse), errors.Is(err, managedinstance.ErrConfigStateConflict),
		errors.Is(err, managedinstance.ErrConfigAlreadyInSync), errors.Is(err, managedinstance.ErrObserveModeWrite),
		errors.Is(err, managedinstance.ErrConfigOperationActive):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrUnsupportedCapability):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "message": "unsupported_capability"})
	case errors.Is(err, managedinstance.ErrCredentialKeyNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "managed instance credential encryption is not configured"})
	default:
		var probeError *managedinstance.ProbeError
		if errors.As(err, &probeError) {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": probeError.Code})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "managed configuration operation failed"})
	}
}
