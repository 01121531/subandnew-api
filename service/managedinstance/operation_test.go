package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var operationTestInstanceSequence atomic.Int64

func setupManagedInstanceOperationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newManagedInstanceTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ManagedInstanceOperation{},
		&model.ManagedInstanceOperationBatch{},
		&model.ManagedInstanceOperationBatchItem{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
		&model.SystemTaskScopeLock{},
	))
	return db
}

func createOperationTestInstance(t *testing.T, mode string, capabilities ...string) *model.ManagedInstance {
	t.Helper()
	sequence := operationTestInstanceSequence.Add(1)
	encodedCapabilities, err := json.Marshal(capabilities)
	require.NoError(t, err)
	instance := &model.ManagedInstance{
		Name: fmt.Sprintf("operation-instance-%d", sequence), Kind: model.ManagedInstanceKindNewAPI,
		BaseURL: fmt.Sprintf("https://managed-%d.example.com", sequence), Environment: "production",
		ManagementMode: mode, TLSVerify: true, Capabilities: string(encodedCapabilities),
	}
	require.NoError(t, model.DB.Create(instance).Error)
	return instance
}

func TestPlanOperationEnforcesObserveCapabilitiesAndIdempotency(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	instance := createOperationTestInstance(t, model.ManagedInstanceModeObserve,
		"channels.list", "channels.test", "channels.toggle")

	key := "inventory-idempotency-secret"
	planned, err := PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key,
		Parameters: json.RawMessage(`{}`), ActorID: 11,
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceOperationStatusPlanned, planned.Status)
	require.False(t, planned.WritesRemote)
	require.False(t, planned.IdempotentReplay)

	replayed, err := PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key,
		Parameters: json.RawMessage(`{}`), ActorID: 11,
	})
	require.NoError(t, err)
	require.Equal(t, planned.OperationId, replayed.OperationId)
	require.True(t, replayed.IdempotentReplay)

	_, err = PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionTestResources, IdempotencyKey: key,
		Parameters: json.RawMessage(`{"resource_ids":[1]}`), ActorID: 11,
	})
	require.ErrorIs(t, err, ErrIdempotencyConflict)

	_, err = PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionToggleResource, IdempotencyKey: "toggle-observe-key",
		Parameters: json.RawMessage(`{"resource_id":1,"enabled":false}`), ActorID: 11,
	})
	require.ErrorIs(t, err, ErrObserveModeWrite)

	require.NoError(t, db.Model(&model.ManagedInstance{}).Where("id = ?", instance.Id).
		Update("capabilities", `["channels.list"]`).Error)
	_, err = PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionTestResources, IdempotencyKey: "missing-capability-key",
		Parameters: json.RawMessage(`{"resource_ids":[1]}`), ActorID: 11,
	})
	require.ErrorIs(t, err, ErrUnsupportedCapability)

	var operations int64
	require.NoError(t, db.Model(&model.ManagedInstanceOperation{}).Count(&operations).Error)
	require.Equal(t, int64(1), operations)
	var storedOperation model.ManagedInstanceOperation
	require.NoError(t, db.First(&storedOperation).Error)
	require.NotEqual(t, key, storedOperation.IdempotencyKey)
	require.Len(t, storedOperation.IdempotencyKey, 64)
	var audits []model.ManagedInstanceAudit
	require.NoError(t, db.Where("instance_id = ?", instance.Id).Find(&audits).Error)
	require.Len(t, audits, 1)
	require.NotContains(t, audits[0].Details, key)
	require.Contains(t, audits[0].Details, planned.IdempotencyFingerprint)

	encodedView, err := json.Marshal(planned)
	require.NoError(t, err)
	require.NotContains(t, string(encodedView), key)
}

func TestExecuteOperationCreatesOneInstanceScopedTask(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	instance := createOperationTestInstance(t, model.ManagedInstanceModeOperate, "channels.toggle")
	_, err := persistObservation(instance.Id, model.ManagedInstanceSnapshotTypeInventory, "channel", 100,
		&InventoryPage{ResourceKind: "channel", Items: []InventoryItem{{ID: 42, Name: "resource"}}, Total: 1}, nil)
	require.NoError(t, err)
	key := "toggle-operation-key"
	planned, err := PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionToggleResource, IdempotencyKey: key,
		Parameters: json.RawMessage(`{"resource_id":42,"enabled":false}`), ActorID: 21,
	})
	require.NoError(t, err)

	queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 22,
	})
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, model.ManagedInstanceOperationStatusQueued, queued.Status)
	require.Equal(t, 22, queued.ExecutedBy)
	require.Equal(t, model.SystemTaskTypeManagedInstanceOperation, task.Type)
	scopeKey := strconv.FormatInt(instance.Id, 10)
	require.Equal(t, scopeKey, task.ScopeKey)
	require.Equal(t, model.SystemTaskTypeManagedInstanceOperation+":"+scopeKey, *task.ActiveKey)
	require.NotContains(t, task.Payload, key)

	replayed, replayedTask, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 22,
	})
	require.NoError(t, err)
	require.Equal(t, queued.OperationId, replayed.OperationId)
	require.Equal(t, task.TaskID, replayedTask.TaskID)
	require.True(t, replayed.IdempotentReplay)

	var taskCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).Where("type = ?", model.SystemTaskTypeManagedInstanceOperation).Count(&taskCount).Error)
	require.Equal(t, int64(1), taskCount)
	var audits []model.ManagedInstanceAudit
	require.NoError(t, db.Where("instance_id = ?", instance.Id).Find(&audits).Error)
	for _, audit := range audits {
		require.NotContains(t, audit.Details, key)
	}
}

func TestExecuteRemoteOperationContracts(t *testing.T) {
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	t.Setenv(managedInstanceAllowedPortsEnv, "*")

	t.Run("new api", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			require.Equal(t, "Bearer new-api-secret", request.Header.Get("Authorization"))
			switch request.URL.Path {
			case "/api/channel/":
				require.Equal(t, http.MethodGet, request.Method)
				_, _ = response.Write([]byte(`{"success":true,"data":[{"id":7},{"id":8}]}`))
			case "/api/channel/test/7":
				require.Equal(t, http.MethodGet, request.Method)
				_, _ = response.Write([]byte(`{"success":true}`))
			case "/api/channel/7/status":
				require.Equal(t, http.MethodPost, request.Method)
				var body map[string]int
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, 2, body["status"])
				_, _ = response.Write([]byte(`{"success":true}`))
			default:
				http.NotFound(response, request)
			}
		}))
		defer server.Close()
		instance := &model.ManagedInstance{Kind: model.ManagedInstanceKindNewAPI, BaseURL: server.URL, TLSVerify: true, RequestTimeoutSeconds: 5}
		credential := &CredentialMaterial{AuthType: "bearer_pat", Secret: "new-api-secret"}

		inventory, err := executeRemoteOperation(context.Background(), instance, credential, model.ManagedInstanceActionRefreshInventory, json.RawMessage(`{}`))
		require.NoError(t, err)
		require.Equal(t, 2, inventory.Count)
		tested, err := executeRemoteOperation(context.Background(), instance, credential, model.ManagedInstanceActionTestResources, json.RawMessage(`{"resource_ids":[7]}`))
		require.NoError(t, err)
		require.Len(t, tested.Items, 1)
		toggled, err := executeRemoteOperation(context.Background(), instance, credential, model.ManagedInstanceActionToggleResource, json.RawMessage(`{"resource_id":7,"enabled":false}`))
		require.NoError(t, err)
		require.False(t, *toggled.Items[0].Enabled)
	})

	t.Run("sub2 api", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			require.Equal(t, "sub2-admin-secret", request.Header.Get("x-api-key"))
			switch request.URL.Path {
			case "/api/v1/admin/accounts":
				require.Equal(t, http.MethodGet, request.Method)
				_, _ = response.Write([]byte(`{"code":0,"data":{"items":[{"id":9}],"total":1}}`))
			case "/api/v1/admin/accounts/9/test":
				require.Equal(t, http.MethodPost, request.Method)
				response.Header().Set("Content-Type", "text/event-stream")
				_, _ = response.Write([]byte("event: done\ndata: {}\n\n"))
			case "/api/v1/admin/accounts/9":
				require.Equal(t, http.MethodPut, request.Method)
				var body map[string]string
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, "inactive", body["status"])
				_, _ = response.Write([]byte(`{"code":0,"data":{}}`))
			default:
				http.NotFound(response, request)
			}
		}))
		defer server.Close()
		instance := &model.ManagedInstance{Kind: model.ManagedInstanceKindSub2API, BaseURL: server.URL, TLSVerify: true, RequestTimeoutSeconds: 5}
		credential := &CredentialMaterial{AuthType: "admin_token", Secret: "sub2-admin-secret"}

		inventory, err := executeRemoteOperation(context.Background(), instance, credential, model.ManagedInstanceActionRefreshInventory, json.RawMessage(`{}`))
		require.NoError(t, err)
		require.Equal(t, 1, inventory.Count)
		tested, err := executeRemoteOperation(context.Background(), instance, credential, model.ManagedInstanceActionTestResources, json.RawMessage(`{"resource_ids":[9]}`))
		require.NoError(t, err)
		require.Len(t, tested.Items, 1)
		toggled, err := executeRemoteOperation(context.Background(), instance, credential, model.ManagedInstanceActionToggleResource, json.RawMessage(`{"resource_id":9,"enabled":false}`))
		require.NoError(t, err)
		require.False(t, *toggled.Items[0].Enabled)
	})
}

func TestRunOperationStoresSanitizedResultAndFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupManagedInstanceOperationTestDB(t)
		instance := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
		planned, err := PlanOperation(instance.Id, PlanOperationInput{
			Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: "run-success-key",
			Parameters: json.RawMessage(`{}`), ActorID: 31,
		})
		require.NoError(t, err)
		queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
			OperationID: planned.OperationId, IdempotencyKey: "run-success-key", ActorID: 32,
		})
		require.NoError(t, err)

		previousExecutor := executeManagedInstanceRemoteOperation
		executeManagedInstanceRemoteOperation = func(_ context.Context, _ *model.ManagedInstance, _ *CredentialMaterial, action string, _ json.RawMessage) (*remoteOperationResult, error) {
			return &remoteOperationResult{Action: action, ResourceKind: "channel", Count: 3}, nil
		}
		t.Cleanup(func() { executeManagedInstanceRemoteOperation = previousExecutor })

		completed, err := RunOperation(context.Background(), queued.OperationId, task.TaskID)
		require.NoError(t, err)
		require.Equal(t, model.ManagedInstanceOperationStatusSucceeded, completed.Status)
		result, ok := completed.Result.(map[string]any)
		require.True(t, ok)
		require.Equal(t, float64(3), result["count"])
	})

	t.Run("failure redacts remote error", func(t *testing.T) {
		db := setupManagedInstanceOperationTestDB(t)
		instance := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
		planned, err := PlanOperation(instance.Id, PlanOperationInput{
			Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: "run-failure-key",
			Parameters: json.RawMessage(`{}`), ActorID: 41,
		})
		require.NoError(t, err)
		queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
			OperationID: planned.OperationId, IdempotencyKey: "run-failure-key", ActorID: 42,
		})
		require.NoError(t, err)

		previousExecutor := executeManagedInstanceRemoteOperation
		executeManagedInstanceRemoteOperation = func(context.Context, *model.ManagedInstance, *CredentialMaterial, string, json.RawMessage) (*remoteOperationResult, error) {
			return nil, errors.New("remote leaked secret token")
		}
		t.Cleanup(func() { executeManagedInstanceRemoteOperation = previousExecutor })

		failed, err := RunOperation(context.Background(), queued.OperationId, task.TaskID)
		var executionError *OperationExecutionError
		require.ErrorAs(t, err, &executionError)
		require.Equal(t, "remote_operation_failed", executionError.Code)
		require.Equal(t, model.ManagedInstanceOperationStatusFailed, failed.Status)
		require.Equal(t, "remote_operation_failed", failed.ErrorCode)
		require.NotContains(t, failed.ErrorCode, "secret")

		var audits []model.ManagedInstanceAudit
		require.NoError(t, db.Where("instance_id = ?", instance.Id).Find(&audits).Error)
		for _, audit := range audits {
			require.NotContains(t, audit.Details, "remote leaked secret token")
		}
	})
}

func TestRemoteWriteResultAmbiguityClassification(t *testing.T) {
	require.False(t, remoteWriteResultMayBeUnknown(true, &ProbeError{Code: ProbeErrorPermission, StatusCode: http.StatusForbidden}))
	require.False(t, remoteWriteResultMayBeUnknown(true, &ProbeError{Code: ProbeErrorAuthentication}))
	require.False(t, remoteWriteResultMayBeUnknown(false, errors.New("connector policy rejected request")))
	require.True(t, remoteWriteResultMayBeUnknown(true, context.DeadlineExceeded))
	require.True(t, remoteWriteResultMayBeUnknown(true, errors.New("connection reset after request write")))
}

func TestRunOperationRejectsRemoteWriteWhenInventoryChangedAfterPlan(t *testing.T) {
	setupManagedInstanceOperationTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var writeCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/channel/":
			writeProbeJSON(response, `{"success":true,"data":[{"id":7,"name":"changed","status":1}]}`)
		case "/api/channel/7/status":
			writeCalls.Add(1)
			writeProbeJSON(response, `{"success":true}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createOperationTestInstance(t, model.ManagedInstanceModeOperate, "channels.list", "channels.toggle")
	require.NoError(t, model.DB.Model(instance).Updates(map[string]any{
		"base_url": server.URL, "environment": "development", "request_timeout_seconds": 5,
	}).Error)
	instance.BaseURL = server.URL
	instance.Environment = "development"
	instance.RequestTimeoutSeconds = 5
	_, err := RotateCredential(instance.Id, CredentialInput{AuthType: "bearer_pat", Secret: "write-secret"}, 1)
	require.NoError(t, err)
	_, err = persistObservation(instance.Id, model.ManagedInstanceSnapshotTypeInventory, "channel", 100,
		&InventoryPage{ResourceKind: "channel", Items: []InventoryItem{{ID: 7, Name: "before", Enabled: boolPointer(true)}}, Total: 1}, nil)
	require.NoError(t, err)

	planned, err := PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionToggleResource, IdempotencyKey: "conflict-operation-key",
		Parameters: json.RawMessage(`{"resource_id":7,"enabled":false}`), ActorID: 1,
	})
	require.NoError(t, err)
	queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: "conflict-operation-key", ActorID: 1,
	})
	require.NoError(t, err)

	failed, err := RunOperation(context.Background(), queued.OperationId, task.TaskID)
	var executionError *OperationExecutionError
	require.ErrorAs(t, err, &executionError)
	require.Equal(t, "remote_conflict", executionError.Code)
	require.Equal(t, model.ManagedInstanceOperationStatusFailed, failed.Status)
	require.Equal(t, int32(0), writeCalls.Load())
}

func TestRunOperationCannotCommitAfterLeaseLoss(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	instance := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	planned, err := PlanOperation(instance.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: "lease-loss-operation-key",
		Parameters: json.RawMessage(`{}`), ActorID: 1,
	})
	require.NoError(t, err)
	queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: "lease-loss-operation-key", ActorID: 1,
	})
	require.NoError(t, err)
	const runnerID = "lease-runner"
	_, claimed, err := model.ClaimScopedSystemTask(task.ID, task.Type, runnerID, time.Now().Unix()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	previousExecutor := executeManagedInstanceRemoteOperation
	executeManagedInstanceRemoteOperation = func(context.Context, *model.ManagedInstance, *CredentialMaterial, string, json.RawMessage) (*remoteOperationResult, error) {
		require.NoError(t, db.Where("task_id = ? AND locked_by = ?", task.TaskID, runnerID).Delete(&model.SystemTaskScopeLock{}).Error)
		return &remoteOperationResult{Action: model.ManagedInstanceActionRefreshInventory, Count: 1}, nil
	}
	t.Cleanup(func() { executeManagedInstanceRemoteOperation = previousExecutor })

	_, err = RunOperationWithLease(context.Background(), queued.OperationId, task.TaskID, runnerID)
	require.ErrorIs(t, err, model.ErrSystemTaskLockLost)
	require.NoError(t, model.MarkSystemTaskLeaseExpired(task.TaskID))

	var operation model.ManagedInstanceOperation
	require.NoError(t, db.Where("operation_id = ?", queued.OperationId).First(&operation).Error)
	require.Equal(t, model.ManagedInstanceOperationStatusFailed, operation.Status)
	require.Equal(t, "task_lease_expired", operation.ErrorCode)
}

func boolPointer(value bool) *bool { return &value }
