package managedinstance

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidOperation       = errors.New("invalid managed instance operation")
	ErrOperationNotFound      = errors.New("managed instance operation not found")
	ErrUnsupportedCapability  = errors.New("managed instance operation capability is not supported")
	ErrObserveModeWrite       = errors.New("managed instance observe mode forbids remote writes")
	ErrIdempotencyConflict    = errors.New("managed instance operation idempotency conflict")
	ErrOperationNotExecutable = errors.New("managed instance operation is not executable")
	ErrOperationBusy          = errors.New("managed instance already has an active operation")
	ErrRemoteConflict         = errors.New("managed instance remote state changed after planning")
)

type PlanOperationInput struct {
	Action         string
	IdempotencyKey string
	Parameters     json.RawMessage
	ActorID        int
}

type ExecuteOperationInput struct {
	OperationID    string
	IdempotencyKey string
	ActorID        int
	BatchID        string
	ExpectedAction string
	RejectedAction string
}

type OperationView struct {
	*model.ManagedInstanceOperation
	Parameters       any  `json:"parameters"`
	Plan             any  `json:"plan"`
	Result           any  `json:"result,omitempty"`
	IdempotentReplay bool `json:"idempotent_replay,omitempty"`
}

type OperationExecutionError struct{ Code string }

func (err *OperationExecutionError) Error() string {
	return "managed instance operation failed: " + err.Code
}

type operationDefinition struct {
	capability string
	writes     bool
}

type operationPlan struct {
	Action             string       `json:"action"`
	RiskLevel          string       `json:"risk_level"`
	WritesRemote       bool         `json:"writes_remote"`
	RequiredCapability string       `json:"required_capability"`
	TargetCount        int          `json:"target_count"`
	Summary            string       `json:"summary"`
	ExpectedETag       string       `json:"expected_etag,omitempty"`
	ExpectedConfigHash string       `json:"expected_config_hash,omitempty"`
	TemplateID         int64        `json:"template_id,omitempty"`
	Differences        []ConfigDiff `json:"differences,omitempty"`
}

type refreshInventoryParameters struct{}

type testResourcesParameters struct {
	ResourceIDs []int64 `json:"resource_ids"`
}

type toggleResourceParameters struct {
	ResourceID int64 `json:"resource_id"`
	Enabled    *bool `json:"enabled"`
}

type applyConfigParameters struct {
	TemplateID    int64          `json:"template_id"`
	SchemaVersion int            `json:"schema_version"`
	ExpectedHash  string         `json:"expected_hash"`
	Desired       map[string]any `json:"desired"`
	Rollback      map[string]any `json:"rollback"`
}

type managedInstanceOperationTaskPayload struct {
	OperationID string `json:"operation_id"`
	InstanceID  int64  `json:"instance_id"`
	ActorID     int    `json:"actor_id"`
	BatchID     string `json:"batch_id,omitempty"`
}

type remoteOperationResult struct {
	Action        string             `json:"action"`
	ResourceKind  string             `json:"resource_kind"`
	Count         int                `json:"count,omitempty"`
	Items         []remoteResultItem `json:"items,omitempty"`
	ChangedFields []string           `json:"changed_fields,omitempty"`
	ObservedHash  string             `json:"observed_hash,omitempty"`
	DesiredHash   string             `json:"desired_hash,omitempty"`
	Verified      bool               `json:"verified,omitempty"`
	Compensated   bool               `json:"compensated,omitempty"`
}

type ConfigApplyError struct {
	Code        string
	Compensated bool
	Result      *remoteOperationResult
	Unknown     bool
}

func (err *ConfigApplyError) Error() string { return "managed config apply failed: " + err.Code }

type remoteResultItem struct {
	ResourceID int64 `json:"resource_id"`
	Succeeded  bool  `json:"succeeded"`
	Enabled    *bool `json:"enabled,omitempty"`
}

type remoteOperationExecutor func(context.Context, *model.ManagedInstance, *CredentialMaterial, string, json.RawMessage) (*remoteOperationResult, error)

var executeManagedInstanceRemoteOperation remoteOperationExecutor = executeRemoteOperation

func PlanOperation(instanceID int64, input PlanOperationInput) (*OperationView, error) {
	if instanceID <= 0 || input.ActorID <= 0 {
		return nil, ErrInvalidOperation
	}
	input.Action = strings.TrimSpace(input.Action)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return nil, fmt.Errorf("%w: idempotency_key must contain 8 to 128 characters", ErrInvalidOperation)
	}

	instance, err := getOperationInstance(instanceID)
	if err != nil {
		return nil, err
	}
	definition, err := operationDefinitionFor(instance.Kind, input.Action)
	if err != nil {
		return nil, err
	}
	if err := authorizeOperation(instance, definition); err != nil {
		return nil, err
	}
	parameters, targetCount, err := normalizeOperationParameters(input.Action, input.Parameters)
	if err != nil {
		return nil, err
	}
	plan := operationPlan{
		Action: input.Action, RiskLevel: "low", WritesRemote: definition.writes,
		RequiredCapability: definition.capability, TargetCount: targetCount,
		Summary: operationSummary(input.Action, targetCount),
	}
	if definition.writes {
		if input.Action != model.ManagedInstanceActionApplyConfig {
			plan.ExpectedETag, err = latestInventoryETag(instanceID, defaultResourceKind(instance.Kind))
			if err != nil {
				return nil, err
			}
		}
	}
	planJSON, _ := json.Marshal(plan)
	planHash := operationPlanHash(input.Action, parameters)

	existing, err := findOperationByIdempotency(instanceID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return matchingIdempotentOperation(existing, planHash)
	}

	operationID, err := generateOperationID()
	if err != nil {
		return nil, err
	}
	operation := &model.ManagedInstanceOperation{
		OperationId: operationID, InstanceId: instanceID, ActorId: input.ActorID,
		Action: input.Action, Status: model.ManagedInstanceOperationStatusPlanned,
		RiskLevel: "low", WritesRemote: definition.writes, RequiredCapability: definition.capability,
		IdempotencyKey: idempotencyDigest(input.IdempotencyKey), IdempotencyFingerprint: idempotencyFingerprint(input.IdempotencyKey),
		PlanHash: planHash, Parameters: string(parameters), Plan: string(planJSON),
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(operation).Error; err != nil {
			return err
		}
		return writeAuditOutcome(tx, instanceID, input.ActorID, "operation_plan", "succeeded", map[string]any{
			"operation_id": operationID, "action": input.Action, "target_count": targetCount,
			"idempotency_fingerprint": operation.IdempotencyFingerprint,
		})
	})
	if err != nil {
		existing, lookupErr := findOperationByIdempotency(instanceID, input.IdempotencyKey)
		if lookupErr == nil && existing != nil {
			return matchingIdempotentOperation(existing, planHash)
		}
		return nil, err
	}
	return operationView(operation), nil
}

func ExecuteOperation(instanceID int64, input ExecuteOperationInput) (*OperationView, *model.SystemTask, error) {
	input.OperationID = strings.TrimSpace(input.OperationID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if instanceID <= 0 || input.ActorID <= 0 || input.OperationID == "" || input.IdempotencyKey == "" {
		return nil, nil, ErrInvalidOperation
	}
	operation, err := getOperationModel(instanceID, input.OperationID)
	if err != nil {
		return nil, nil, err
	}
	if input.ExpectedAction != "" && operation.Action != input.ExpectedAction ||
		input.RejectedAction != "" && operation.Action == input.RejectedAction {
		return nil, nil, ErrOperationNotFound
	}
	if !sameSecret(operation.IdempotencyKey, idempotencyDigest(input.IdempotencyKey)) {
		return nil, nil, ErrIdempotencyConflict
	}
	instance, err := getOperationInstance(instanceID)
	if err != nil {
		return nil, nil, err
	}
	definition, err := operationDefinitionFor(instance.Kind, operation.Action)
	if err != nil {
		return nil, nil, err
	}
	if definition.capability != operation.RequiredCapability || definition.writes != operation.WritesRemote {
		return nil, nil, ErrOperationNotExecutable
	}
	if err := authorizeOperation(instance, definition); err != nil {
		return nil, nil, err
	}
	if operation.Action == model.ManagedInstanceActionApplyConfig {
		if err := authorizeConfigApplyBinding(instanceID, operation.Parameters); err != nil {
			return nil, nil, err
		}
	}
	if operation.Status != model.ManagedInstanceOperationStatusPlanned {
		task, taskErr := operationTask(operation.TaskId)
		view := operationView(operation)
		view.IdempotentReplay = true
		return view, task, taskErr
	}

	taskID, err := model.GenerateSystemTaskID()
	if err != nil {
		return nil, nil, err
	}
	payload, _ := json.Marshal(managedInstanceOperationTaskPayload{
		OperationID: operation.OperationId, InstanceID: instanceID, ActorID: input.ActorID,
		BatchID: strings.TrimSpace(input.BatchID),
	})
	scopeKey := strconv.FormatInt(instanceID, 10)
	activeKey := model.SystemTaskTypeManagedInstanceOperation + ":" + scopeKey
	task := &model.SystemTask{
		TaskID: taskID, Type: model.SystemTaskTypeManagedInstanceOperation, ScopeKey: scopeKey,
		Status: model.SystemTaskStatusPending, ActiveKey: &activeKey, Payload: string(payload), State: "{}",
	}
	now := common.GetTimestamp()
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ManagedInstanceOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND instance_id = ?", operation.Id, instanceID).First(&current).Error; err != nil {
			return err
		}
		if current.Status != model.ManagedInstanceOperationStatusPlanned {
			return ErrOperationNotExecutable
		}
		if err := tx.Create(task).Error; err != nil {
			return ErrOperationBusy
		}
		result := tx.Model(&model.ManagedInstanceOperation{}).
			Where("id = ? AND status = ?", operation.Id, model.ManagedInstanceOperationStatusPlanned).
			Updates(map[string]any{
				"task_id": taskID, "status": model.ManagedInstanceOperationStatusQueued,
				"executed_by": input.ActorID, "executed_at": now, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOperationNotExecutable
		}
		return writeAuditOutcome(tx, instanceID, input.ActorID, "operation_execute", "queued", map[string]any{
			"operation_id": operation.OperationId, "action": operation.Action, "task_id": taskID,
			"idempotency_fingerprint": operation.IdempotencyFingerprint,
		})
	})
	if err != nil {
		latest, lookupErr := getOperationModel(instanceID, input.OperationID)
		if lookupErr == nil && latest.Status != model.ManagedInstanceOperationStatusPlanned {
			existingTask, taskErr := operationTask(latest.TaskId)
			view := operationView(latest)
			view.IdempotentReplay = true
			return view, existingTask, taskErr
		}
		return nil, nil, err
	}
	operation, err = getOperationModel(instanceID, input.OperationID)
	if err != nil {
		return nil, nil, err
	}
	return operationView(operation), task, nil
}

func GetOperation(instanceID int64, operationID string) (*OperationView, error) {
	operation, err := getOperationModel(instanceID, strings.TrimSpace(operationID))
	if err != nil {
		return nil, err
	}
	return operationView(operation), nil
}

func FailQueuedOperation(operationID string, taskID string, actorID int, errorCode string) error {
	if strings.TrimSpace(operationID) == "" || strings.TrimSpace(taskID) == "" || actorID <= 0 || errorCode == "" {
		return ErrInvalidOperation
	}
	now := common.GetTimestamp()
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var operation model.ManagedInstanceOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("operation_id = ? AND task_id = ?", operationID, taskID).First(&operation).Error; err != nil {
			return err
		}
		if operation.Status != model.ManagedInstanceOperationStatusQueued {
			return nil
		}
		if err := tx.Model(&model.ManagedInstanceOperation{}).Where("id = ?", operation.Id).Updates(map[string]any{
			"status": model.ManagedInstanceOperationStatusFailed, "error_code": errorCode,
			"finished_at": now, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		return writeAuditOutcome(tx, operation.InstanceId, actorID, "operation_complete", "failed", map[string]any{
			"operation_id": operation.OperationId, "action": operation.Action, "error_code": errorCode,
		})
	})
}

func RunOperation(ctx context.Context, operationID string, taskID string) (*OperationView, error) {
	return runOperation(ctx, operationID, taskID, "")
}

func RunOperationWithLease(ctx context.Context, operationID string, taskID string, runnerID string) (*OperationView, error) {
	return runOperation(ctx, operationID, taskID, runnerID)
}

func runOperation(ctx context.Context, operationID string, taskID string, runnerID string) (*OperationView, error) {
	var operation model.ManagedInstanceOperation
	if err := model.DB.Where("operation_id = ? AND task_id = ?", operationID, taskID).First(&operation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOperationNotFound
		}
		return nil, err
	}
	if operation.Status != model.ManagedInstanceOperationStatusQueued {
		return nil, ErrOperationNotExecutable
	}
	if err := requireOperationLease(taskID, runnerID); err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	result := model.DB.Model(&model.ManagedInstanceOperation{}).
		Where("id = ? AND status = ?", operation.Id, model.ManagedInstanceOperationStatusQueued).
		Updates(map[string]any{"status": model.ManagedInstanceOperationStatusRunning, "updated_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrOperationNotExecutable
	}
	operation.Status = model.ManagedInstanceOperationStatusRunning

	instance, err := getOperationInstance(operation.InstanceId)
	if err == nil {
		definition, definitionErr := operationDefinitionFor(instance.Kind, operation.Action)
		if definitionErr != nil {
			err = definitionErr
		} else {
			err = authorizeOperation(instance, definition)
		}
		if err == nil && operation.Action == model.ManagedInstanceActionApplyConfig {
			err = authorizeConfigApplyBinding(operation.InstanceId, operation.Parameters)
		}
	}
	var remoteResult *remoteOperationResult
	var remoteWriteSent atomic.Bool
	if err == nil {
		var credential *CredentialMaterial
		credential, err = loadCredential(operation.InstanceId)
		if err == nil {
			if operation.WritesRemote && operation.Action != model.ManagedInstanceActionApplyConfig {
				err = verifyOperationInventoryETag(ctx, &operation, instance)
			}
		}
		if err == nil {
			err = requireOperationLease(taskID, runnerID)
		}
		if err == nil {
			executionContext := ctx
			if operation.WritesRemote && operation.Action != model.ManagedInstanceActionApplyConfig {
				executionContext = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
					WroteRequest: func(httptrace.WroteRequestInfo) { remoteWriteSent.Store(true) },
				})
			}
			remoteResult, err = executeManagedInstanceRemoteOperation(executionContext, instance, credential, operation.Action, json.RawMessage(operation.Parameters))
		}
	}
	if err != nil {
		code := managedInstanceOperationErrorCode(err)
		status := model.ManagedInstanceOperationStatusFailed
		if remoteWriteResultMayBeUnknown(remoteWriteSent.Load(), err) {
			status = model.ManagedInstanceOperationStatusUnknown
			code = "remote_result_unknown"
		}
		var failureResult json.RawMessage
		var configApplyError *ConfigApplyError
		if errors.As(err, &configApplyError) && configApplyError.Result != nil {
			failureResult, _ = json.Marshal(configApplyError.Result)
		}
		if finishErr := finishOperation(&operation, failureResult, code, status, taskID, runnerID); finishErr != nil {
			return nil, finishErr
		}
		return operationView(&operation), &OperationExecutionError{Code: code}
	}
	encodedResult, err := json.Marshal(remoteResult)
	if err != nil {
		return nil, err
	}
	if err := finishOperation(&operation, encodedResult, "", model.ManagedInstanceOperationStatusSucceeded, taskID, runnerID); err != nil {
		return nil, err
	}
	return operationView(&operation), nil
}

func remoteWriteResultMayBeUnknown(requestWritten bool, err error) bool {
	var configApplyError *ConfigApplyError
	if errors.As(err, &configApplyError) && configApplyError.Compensated {
		return false
	}
	if errors.As(err, &configApplyError) && configApplyError.Unknown {
		return true
	}
	var configWriteUnknown *remoteConfigWriteUnknownError
	if errors.As(err, &configWriteUnknown) {
		return true
	}
	var probeError *ProbeError
	return requestWritten && err != nil && !errors.As(err, &probeError)
}

func finishOperation(operation *model.ManagedInstanceOperation, result json.RawMessage, errorCode string, status string, taskID string, runnerID string) error {
	now := common.GetTimestamp()
	outcome := status
	if status == model.ManagedInstanceOperationStatusUnknown {
		outcome = "unknown"
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if runnerID != "" {
			if err := model.RequireValidSystemTaskLease(tx, taskID, runnerID, now); err != nil {
				return err
			}
		}
		updates := map[string]any{
			"status": status, "result": string(result), "error_code": errorCode,
			"finished_at": now, "updated_at": now,
		}
		update := tx.Model(&model.ManagedInstanceOperation{}).
			Where("id = ? AND task_id = ? AND status = ?", operation.Id, taskID, model.ManagedInstanceOperationStatusRunning).
			Updates(updates)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return model.ErrSystemTaskLockLost
		}
		operation.Status = status
		operation.Result = string(result)
		operation.ErrorCode = errorCode
		operation.FinishedAt = now
		if operation.Action == model.ManagedInstanceActionApplyConfig && status == model.ManagedInstanceOperationStatusSucceeded {
			if err := markConfigApplyCompleted(tx, operation.InstanceId, operation.Parameters, result); err != nil {
				return err
			}
		}
		details := map[string]any{"operation_id": operation.OperationId, "action": operation.Action}
		if errorCode != "" {
			details["error_code"] = errorCode
		}
		actorID := operation.ExecutedBy
		if actorID == 0 {
			actorID = operation.ActorId
		}
		return writeAuditOutcome(tx, operation.InstanceId, actorID, "operation_complete", outcome, details)
	})
}

func requireOperationLease(taskID string, runnerID string) error {
	if runnerID == "" {
		return nil
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		return model.RequireValidSystemTaskLease(tx, taskID, runnerID, common.GetTimestamp())
	})
}

func executeRemoteOperation(ctx context.Context, instance *model.ManagedInstance, credential *CredentialMaterial, action string, parameters json.RawMessage) (*remoteOperationResult, error) {
	if credential == nil {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	policy, err := ConnectorPolicyFromEnvironment()
	if err != nil {
		return nil, err
	}
	connector, err := NewConnector(instance, policy)
	if err != nil {
		return nil, err
	}
	switch instance.Kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		headers, err := newAPIAuthHeaders(ctx, connector, instance.Kind, credential)
		if err != nil {
			return nil, err
		}
		return executeNewAPIOperation(ctx, connector, instance, headers, action, parameters)
	case model.ManagedInstanceKindSub2API:
		headers, err := sub2APIAuthHeaders(ctx, connector, credential)
		if err != nil {
			return nil, err
		}
		return executeSub2APIOperation(ctx, connector, instance, headers, action, parameters)
	default:
		return nil, ErrUnsupportedCapability
	}
}

func executeNewAPIOperation(ctx context.Context, connector *Connector, instance *model.ManagedInstance, headers http.Header, action string, parameters json.RawMessage) (*remoteOperationResult, error) {
	switch action {
	case model.ManagedInstanceActionRefreshInventory:
		response, err := connector.DoJSON(ctx, http.MethodGet, "/api/channel/", headers, nil)
		if err != nil {
			return nil, err
		}
		count, err := newAPIInventoryCount(response)
		return &remoteOperationResult{Action: action, ResourceKind: "channel", Count: count}, err
	case model.ManagedInstanceActionTestResources:
		var params testResourcesParameters
		_ = json.Unmarshal(parameters, &params)
		items := make([]remoteResultItem, 0, len(params.ResourceIDs))
		for _, id := range params.ResourceIDs {
			response, err := connector.DoJSON(ctx, http.MethodGet, "/api/channel/test/"+strconv.FormatInt(id, 10), headers, nil)
			if err != nil {
				return nil, err
			}
			if err := requireNewAPISuccess(response); err != nil {
				return nil, err
			}
			items = append(items, remoteResultItem{ResourceID: id, Succeeded: true})
		}
		return &remoteOperationResult{Action: action, ResourceKind: "channel", Count: len(items), Items: items}, nil
	case model.ManagedInstanceActionToggleResource:
		var params toggleResourceParameters
		_ = json.Unmarshal(parameters, &params)
		status := 2
		if params.Enabled != nil && *params.Enabled {
			status = 1
		}
		response, err := connector.DoJSON(ctx, http.MethodPost, "/api/channel/"+strconv.FormatInt(params.ResourceID, 10)+"/status", headers, map[string]any{"status": status})
		if err != nil {
			return nil, err
		}
		if err := requireNewAPISuccess(response); err != nil {
			return nil, err
		}
		return &remoteOperationResult{Action: action, ResourceKind: "channel", Count: 1, Items: []remoteResultItem{{ResourceID: params.ResourceID, Succeeded: true, Enabled: params.Enabled}}}, nil
	case model.ManagedInstanceActionApplyConfig:
		return executeConfigApply(ctx, connector, instance, headers, parameters)
	default:
		return nil, ErrInvalidOperation
	}
}

func executeSub2APIOperation(ctx context.Context, connector *Connector, instance *model.ManagedInstance, headers http.Header, action string, parameters json.RawMessage) (*remoteOperationResult, error) {
	switch action {
	case model.ManagedInstanceActionRefreshInventory:
		response, err := connector.DoJSON(ctx, http.MethodGet, "/api/v1/admin/accounts", headers, nil)
		if err != nil {
			return nil, err
		}
		count, err := sub2InventoryCount(response)
		return &remoteOperationResult{Action: action, ResourceKind: "account", Count: count}, err
	case model.ManagedInstanceActionTestResources:
		var params testResourcesParameters
		_ = json.Unmarshal(parameters, &params)
		items := make([]remoteResultItem, 0, len(params.ResourceIDs))
		for _, id := range params.ResourceIDs {
			response, err := connector.DoJSON(ctx, http.MethodPost, "/api/v1/admin/accounts/"+strconv.FormatInt(id, 10)+"/test", headers, nil)
			if err != nil {
				return nil, err
			}
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return nil, probeHTTPError(response.StatusCode)
			}
			items = append(items, remoteResultItem{ResourceID: id, Succeeded: true})
		}
		return &remoteOperationResult{Action: action, ResourceKind: "account", Count: len(items), Items: items}, nil
	case model.ManagedInstanceActionToggleResource:
		var params toggleResourceParameters
		_ = json.Unmarshal(parameters, &params)
		status := "inactive"
		if params.Enabled != nil && *params.Enabled {
			status = "active"
		}
		response, err := connector.DoJSON(ctx, http.MethodPut, "/api/v1/admin/accounts/"+strconv.FormatInt(params.ResourceID, 10), headers, map[string]any{"status": status})
		if err != nil {
			return nil, err
		}
		if err := requireSub2Success(response); err != nil {
			return nil, err
		}
		return &remoteOperationResult{Action: action, ResourceKind: "account", Count: 1, Items: []remoteResultItem{{ResourceID: params.ResourceID, Succeeded: true, Enabled: params.Enabled}}}, nil
	case model.ManagedInstanceActionApplyConfig:
		return executeConfigApply(ctx, connector, instance, headers, parameters)
	default:
		return nil, ErrInvalidOperation
	}
}

func operationDefinitionFor(kind string, action string) (operationDefinition, error) {
	prefix := ""
	switch kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		prefix = "channels"
	case model.ManagedInstanceKindSub2API:
		prefix = "accounts"
	default:
		return operationDefinition{}, ErrUnsupportedCapability
	}
	switch action {
	case model.ManagedInstanceActionRefreshInventory:
		return operationDefinition{capability: prefix + ".list"}, nil
	case model.ManagedInstanceActionTestResources:
		return operationDefinition{capability: prefix + ".test"}, nil
	case model.ManagedInstanceActionToggleResource:
		return operationDefinition{capability: prefix + ".toggle", writes: true}, nil
	case model.ManagedInstanceActionApplyConfig:
		return operationDefinition{capability: "config.apply", writes: true}, nil
	default:
		return operationDefinition{}, fmt.Errorf("%w: unsupported action", ErrInvalidOperation)
	}
}

func authorizeOperation(instance *model.ManagedInstance, definition operationDefinition) error {
	if definition.writes && instance.ManagementMode == model.ManagedInstanceModeObserve {
		return ErrObserveModeWrite
	}
	var capabilities []string
	if err := json.Unmarshal([]byte(instance.Capabilities), &capabilities); err != nil {
		return ErrUnsupportedCapability
	}
	for _, capability := range capabilities {
		if capability == definition.capability {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrUnsupportedCapability, definition.capability)
}

func normalizeOperationParameters(action string, raw json.RawMessage) (json.RawMessage, int, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = json.RawMessage("{}")
	}
	switch action {
	case model.ManagedInstanceActionRefreshInventory:
		var params refreshInventoryParameters
		if err := decodeStrictJSON(raw, &params); err != nil {
			return nil, 0, fmt.Errorf("%w: parameters", ErrInvalidOperation)
		}
		encoded, _ := json.Marshal(params)
		return encoded, 0, nil
	case model.ManagedInstanceActionTestResources:
		var params testResourcesParameters
		if err := decodeStrictJSON(raw, &params); err != nil || len(params.ResourceIDs) == 0 || len(params.ResourceIDs) > 20 {
			return nil, 0, fmt.Errorf("%w: resource_ids must contain 1 to 20 IDs", ErrInvalidOperation)
		}
		seen := make(map[int64]struct{}, len(params.ResourceIDs))
		for _, id := range params.ResourceIDs {
			if id <= 0 {
				return nil, 0, fmt.Errorf("%w: resource IDs must be positive", ErrInvalidOperation)
			}
			if _, exists := seen[id]; exists {
				return nil, 0, fmt.Errorf("%w: resource IDs must be unique", ErrInvalidOperation)
			}
			seen[id] = struct{}{}
		}
		encoded, _ := json.Marshal(params)
		return encoded, len(params.ResourceIDs), nil
	case model.ManagedInstanceActionToggleResource:
		var params toggleResourceParameters
		if err := decodeStrictJSON(raw, &params); err != nil || params.ResourceID <= 0 || params.Enabled == nil {
			return nil, 0, fmt.Errorf("%w: resource_id and enabled are required", ErrInvalidOperation)
		}
		encoded, _ := json.Marshal(params)
		return encoded, 1, nil
	case model.ManagedInstanceActionApplyConfig:
		return nil, 0, fmt.Errorf("%w: use the config apply planning endpoint", ErrInvalidOperation)
	default:
		return nil, 0, fmt.Errorf("%w: unsupported action", ErrInvalidOperation)
	}
}

func decodeStrictJSON(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func newAPIInventoryCount(response *ConnectorResponse) (int, error) {
	if err := requireHTTPStatus(response); err != nil {
		return 0, err
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !envelope.Success {
		return 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return countInventoryItems(envelope.Data)
}

func sub2InventoryCount(response *ConnectorResponse) (int, error) {
	if err := requireHTTPStatus(response); err != nil {
		return 0, err
	}
	var envelope struct {
		Code any             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !sub2SuccessCode(envelope.Code) {
		return 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return countInventoryItems(envelope.Data)
}

func countInventoryItems(data json.RawMessage) (int, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err == nil {
		return len(items), nil
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
		Data  []json.RawMessage `json:"data"`
		Total *int              `json:"total"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return 0, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	if page.Total != nil {
		return *page.Total, nil
	}
	if page.Items != nil {
		return len(page.Items), nil
	}
	if page.Data != nil {
		return len(page.Data), nil
	}
	return 0, &ProbeError{Code: ProbeErrorInvalidResponse}
}

func requireNewAPISuccess(response *ConnectorResponse) error {
	if err := requireHTTPStatus(response); err != nil {
		return err
	}
	var envelope struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !envelope.Success {
		return &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return nil
}

func requireSub2Success(response *ConnectorResponse) error {
	if err := requireHTTPStatus(response); err != nil {
		return err
	}
	var envelope struct {
		Code any `json:"code"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !sub2SuccessCode(envelope.Code) {
		return &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return nil
}

func requireHTTPStatus(response *ConnectorResponse) error {
	if response == nil {
		return &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return probeHTTPError(response.StatusCode)
	}
	return nil
}

func getOperationInstance(instanceID int64) (*model.ManagedInstance, error) {
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}
	return &instance, nil
}

func getOperationModel(instanceID int64, operationID string) (*model.ManagedInstanceOperation, error) {
	var operation model.ManagedInstanceOperation
	if operationID == "" {
		return nil, ErrInvalidOperation
	}
	if err := model.DB.Where("instance_id = ? AND operation_id = ?", instanceID, operationID).First(&operation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOperationNotFound
		}
		return nil, err
	}
	return &operation, nil
}

func findOperationByIdempotency(instanceID int64, key string) (*model.ManagedInstanceOperation, error) {
	var operation model.ManagedInstanceOperation
	err := model.DB.Where("instance_id = ? AND idempotency_key = ?", instanceID, idempotencyDigest(key)).First(&operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &operation, nil
}

func matchingIdempotentOperation(operation *model.ManagedInstanceOperation, planHash string) (*OperationView, error) {
	if operation.PlanHash != planHash {
		return nil, ErrIdempotencyConflict
	}
	view := operationView(operation)
	view.IdempotentReplay = true
	return view, nil
}

func operationTask(taskID string) (*model.SystemTask, error) {
	if taskID == "" {
		return nil, nil
	}
	return model.GetSystemTaskByTaskID(taskID)
}

func operationView(operation *model.ManagedInstanceOperation) *OperationView {
	if operation == nil {
		return nil
	}
	view := &OperationView{ManagedInstanceOperation: operation}
	_ = json.Unmarshal([]byte(operation.Parameters), &view.Parameters)
	_ = json.Unmarshal([]byte(operation.Plan), &view.Plan)
	if operation.Result != "" {
		_ = json.Unmarshal([]byte(operation.Result), &view.Result)
	}
	return view
}

func operationPlanHash(action string, parameters json.RawMessage) string {
	digest := sha256.Sum256(append([]byte(action+"\n"), parameters...))
	return hex.EncodeToString(digest[:])
}

func idempotencyFingerprint(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:8])
}

func idempotencyDigest(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

func sameSecret(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func generateOperationID() (string, error) {
	key, err := common.GenerateRandomCharsKey(24)
	if err != nil {
		return "", err
	}
	return "miop_" + key, nil
}

func operationSummary(action string, targetCount int) string {
	switch action {
	case model.ManagedInstanceActionRefreshInventory:
		return "Refresh the remote resource inventory summary"
	case model.ManagedInstanceActionTestResources:
		return fmt.Sprintf("Test %d remote resources", targetCount)
	case model.ManagedInstanceActionToggleResource:
		return "Change one remote resource enabled state"
	default:
		return "Managed instance operation"
	}
}

func managedInstanceOperationErrorCode(err error) string {
	var configApplyError *ConfigApplyError
	if errors.As(err, &configApplyError) {
		return configApplyError.Code
	}
	var executionError *OperationExecutionError
	if errors.As(err, &executionError) && executionError.Code != "" {
		return executionError.Code
	}
	var probeError *ProbeError
	if errors.As(err, &probeError) {
		return probeError.Code
	}
	switch {
	case errors.Is(err, ErrObserveModeWrite):
		return "observe_mode_write_forbidden"
	case errors.Is(err, ErrUnsupportedCapability):
		return "unsupported_capability"
	case errors.Is(err, ErrRemoteConflict):
		return "remote_conflict"
	case errors.Is(err, ErrCredentialKeyNotConfigured):
		return "credential_key_not_configured"
	case errors.Is(err, ErrConnectorTargetBlocked):
		return "target_blocked"
	case errors.Is(err, ErrConnectorRedirect):
		return "redirect_blocked"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "operation_cancelled"
	default:
		return "remote_operation_failed"
	}
}

func latestInventoryETag(instanceID int64, resourceKind string) (string, error) {
	var snapshot model.ManagedInstanceSnapshot
	err := model.DB.Where(
		"instance_id = ? AND snapshot_type = ? AND resource_kind = ? AND collection_status = ?",
		instanceID, model.ManagedInstanceSnapshotTypeInventory, resourceKind, model.ManagedInstanceCollectionSucceeded,
	).Order("observed_at desc, id desc").First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("%w: refresh inventory before planning a remote write", ErrOperationNotExecutable)
	}
	if err != nil {
		return "", err
	}
	if snapshot.ETag == "" {
		return "", ErrOperationNotExecutable
	}
	return snapshot.ETag, nil
}

func verifyOperationInventoryETag(ctx context.Context, operation *model.ManagedInstanceOperation, instance *model.ManagedInstance) error {
	var plan operationPlan
	if err := json.Unmarshal([]byte(operation.Plan), &plan); err != nil || plan.ExpectedETag == "" {
		return ErrOperationNotExecutable
	}
	observation, err := CollectInventory(ctx, instance.Id, defaultResourceKind(instance.Kind), "")
	if err != nil {
		return err
	}
	if observation.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
		return &OperationExecutionError{Code: observation.ErrorCode}
	}
	if observation.ETag != plan.ExpectedETag {
		return ErrRemoteConflict
	}
	return nil
}
