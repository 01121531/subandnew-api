package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/i18n"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
)

type managedInstanceOperationPlanRequest struct {
	Action         string          `json:"action"`
	IdempotencyKey string          `json:"idempotency_key"`
	Parameters     json.RawMessage `json:"parameters"`
}

type managedInstanceOperationExecuteRequest struct {
	OperationID    string `json:"operation_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type managedInstanceBatchTargetRequest struct {
	InstanceID int64           `json:"instance_id"`
	Parameters json.RawMessage `json:"parameters"`
}

type managedInstanceBatchPlanRequest struct {
	Action         string                              `json:"action"`
	IdempotencyKey string                              `json:"idempotency_key"`
	Targets        []managedInstanceBatchTargetRequest `json:"targets"`
}

type managedInstanceBatchExecuteRequest struct {
	BatchID        string `json:"batch_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// Managed instance operation API draft (v1):
//
//	POST /api/managed-instances/:id/actions/plan
//	{"action":"refresh_inventory|test_resources|toggle_resource",
//	 "idempotency_key":"client-generated-key","parameters":{...}}
//	-> 201 with a durable immutable operation plan; an identical idempotent
//	   retry returns 200 and the original plan.
//
//	POST /api/managed-instances/:id/actions
//	{"operation_id":"miop_...","idempotency_key":"same-key-as-plan"}
//	-> 202 with {operation, task}; execution is asynchronous and scoped by
//	   instance. Repeating the request returns the original operation/task.
//
//	GET /api/managed-instances/:id/operations/:operation_id
//	-> 200 with plan, sanitized parameters, status, safe error code and
//	   sanitized result. Idempotency keys, credentials and raw remote bodies
//	   are never returned.
func PlanManagedInstanceOperation(c *gin.Context) {
	instanceID, ok := managedInstanceID(c)
	if !ok {
		return
	}
	var request managedInstanceOperationPlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid operation plan request"})
		return
	}
	operation, err := managedinstance.PlanOperation(instanceID, managedinstance.PlanOperationInput{
		Action: request.Action, IdempotencyKey: request.IdempotencyKey,
		Parameters: request.Parameters, ActorID: c.GetInt("id"),
	})
	if err != nil {
		managedInstanceOperationError(c, err)
		return
	}
	status := http.StatusCreated
	if operation.IdempotentReplay {
		status = http.StatusOK
	}
	adminManagedInstanceDTOJSON(c, status, operation)
}

func ExecuteManagedInstanceOperation(c *gin.Context) {
	executeManagedInstanceOperation(c, "", model.ManagedInstanceActionApplyConfig)
}

func executeManagedInstanceOperation(c *gin.Context, expectedAction string, rejectedAction string) {
	instanceID, ok := managedInstanceID(c)
	if !ok {
		return
	}
	var request managedInstanceOperationExecuteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid operation execute request"})
		return
	}
	operation, task, err := managedinstance.ExecuteOperation(instanceID, managedinstance.ExecuteOperationInput{
		OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey, ActorID: c.GetInt("id"),
		ExpectedAction: expectedAction, RejectedAction: rejectedAction,
	})
	if err != nil {
		managedInstanceOperationError(c, err)
		return
	}
	data := managedinstance.OperationTaskView{Operation: operation}
	if task != nil {
		response := task.ToResponse()
		data.Task = &response
	}
	adminManagedInstanceDTOJSON(c, http.StatusAccepted, data)
}

func GetManagedInstanceOperation(c *gin.Context) {
	getManagedInstanceOperation(c, "", model.ManagedInstanceActionApplyConfig)
}

func getManagedInstanceOperation(c *gin.Context, expectedAction string, rejectedAction string) {
	instanceID, ok := managedInstanceID(c)
	if !ok {
		return
	}
	operation, err := managedinstance.GetOperation(instanceID, c.Param("operation_id"))
	if err != nil {
		managedInstanceOperationError(c, err)
		return
	}
	if expectedAction != "" && operation.Action != expectedAction || rejectedAction != "" && operation.Action == rejectedAction {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "managed instance operation not found"})
		return
	}
	userID := c.GetInt("id")
	canAudit := authz.Can(userID, c.GetInt("role"), authz.ManagedInstanceAudit)
	if !canAudit && operation.ActorId != userID && operation.ExecutedBy != userID {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		return
	}
	adminManagedInstanceDTOJSON(c, http.StatusOK, operation)
}

func PlanManagedInstanceBatchOperation(c *gin.Context) {
	var request managedInstanceBatchPlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid batch operation plan request"})
		return
	}
	ids := make([]int64, 0, len(request.Targets))
	for _, target := range request.Targets {
		ids = append(ids, target.InstanceID)
	}
	if !adminInstancesAllowed(c, ids) {
		return
	}
	targets := make([]managedinstance.BatchOperationTargetInput, 0, len(request.Targets))
	for _, target := range request.Targets {
		targets = append(targets, managedinstance.BatchOperationTargetInput{
			InstanceID: target.InstanceID, Parameters: target.Parameters,
		})
	}
	batch, err := managedinstance.PlanBatchOperation(managedinstance.PlanBatchOperationInput{
		Action: request.Action, IdempotencyKey: request.IdempotencyKey,
		Targets: targets, ActorID: c.GetInt("id"),
	})
	if err != nil {
		managedInstanceOperationError(c, err)
		return
	}
	status := http.StatusCreated
	if batch.IdempotentReplay {
		status = http.StatusOK
	}
	adminManagedInstanceDTOJSON(c, status, batch)
}

func ExecuteManagedInstanceBatchOperation(c *gin.Context) {
	var request managedInstanceBatchExecuteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid batch operation execute request"})
		return
	}
	if !adminBatchAllowed(c, request.BatchID) {
		return
	}
	batch, err := managedinstance.ExecuteBatchOperation(managedinstance.ExecuteBatchOperationInput{
		BatchID: request.BatchID, IdempotencyKey: request.IdempotencyKey, ActorID: c.GetInt("id"),
	})
	if err != nil {
		managedInstanceOperationError(c, err)
		return
	}
	adminManagedInstanceDTOJSON(c, http.StatusAccepted, batch)
}

func GetManagedInstanceBatchOperation(c *gin.Context) {
	if !adminBatchAllowed(c, c.Param("batch_id")) {
		return
	}
	batch, err := managedinstance.GetBatchOperation(c.Param("batch_id"))
	if err != nil {
		managedInstanceOperationError(c, err)
		return
	}
	userID := c.GetInt("id")
	canAudit := authz.Can(userID, c.GetInt("role"), authz.ManagedInstanceAudit)
	if !canAudit && batch.ActorId != userID && batch.ExecutedBy != userID {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		return
	}
	adminManagedInstanceDTOJSON(c, http.StatusOK, batch)
}

func adminBatchAllowed(c *gin.Context, batchID string) bool {
	var ids []int64
	if err := model.DB.Model(&model.ManagedInstanceOperationBatchItem{}).Where("batch_id = ?", strings.TrimSpace(batchID)).Pluck("instance_id", &ids).Error; err != nil {
		managedInstanceOperationError(c, err)
		return false
	}
	return adminInstancesAllowed(c, ids)
}

func managedInstanceOperationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrDataForbidden), errors.Is(err, authz.ErrAuthorizationChanged):
		adminDataError(c, err)
	case errors.Is(err, managedinstance.ErrInvalidOperation):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrInstanceNotFound),
		errors.Is(err, managedinstance.ErrOperationNotFound),
		errors.Is(err, managedinstance.ErrBatchOperationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "managed instance operation not found"})
	case errors.Is(err, managedinstance.ErrIdempotencyConflict),
		errors.Is(err, managedinstance.ErrOperationNotExecutable),
		errors.Is(err, managedinstance.ErrOperationBusy),
		errors.Is(err, managedinstance.ErrObserveModeWrite):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrUnsupportedCapability):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "message": "unsupported_capability"})
	case errors.Is(err, managedinstance.ErrCredentialKeyNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "managed instance credential encryption is not configured"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "managed instance operation failed"})
	}
}
