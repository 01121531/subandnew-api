package managedinstance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestConfigSchemaRejectsUnknownAndOperationalWriteFields(t *testing.T) {
	values, err := validateConfigValues(model.ManagedInstanceKindNewAPI, 1, json.RawMessage(`{
		"ui.site_name":"Control Plane","ui.logo_url":"https://example.com/logo.png"
	}`))
	require.NoError(t, err)
	require.Equal(t, "Control Plane", values["ui.site_name"])

	_, err = validateConfigValues(model.ManagedInstanceKindNewAPI, 1, json.RawMessage(`{"RetryTimes":2}`))
	require.ErrorIs(t, err, ErrInvalidConfigTemplate)
	_, err = validateConfigValues(model.ManagedInstanceKindSub2API, 1, json.RawMessage(`{"payment_enabled":true}`))
	require.ErrorIs(t, err, ErrInvalidConfigTemplate)
	_, err = validateConfigValues(model.ManagedInstanceKindSub2API, 1, json.RawMessage(`{"ui.docs_url":"http://insecure.example.com"}`))
	require.ErrorIs(t, err, ErrInvalidConfigTemplate)
	_, err = validateConfigValues(model.ManagedInstanceKindNewAPI, 1, json.RawMessage(`{"ui.site_name":""}`))
	require.ErrorIs(t, err, ErrInvalidConfigTemplate)
}

func TestConfigTemplateBindingDefaultsToExplicitAudit(t *testing.T) {
	newManagedInstanceTestDB(t)
	instance := createConfigTestInstance(t, "https://config.example.com", model.ManagedInstanceModeObserve)
	template, err := CreateConfigTemplate(ConfigTemplateInput{
		Name: "Public branding", Kind: model.ManagedInstanceKindNewAPI, SchemaVersion: 1,
		Values: json.RawMessage(`{"ui.site_name":"Managed API"}`), ActorID: 1,
	})
	require.NoError(t, err)
	binding, err := SetConfigBinding(instance.Id, ConfigBindingInput{TemplateID: template.Id, Mode: model.ManagedConfigModeAudit, ActorID: 1})
	require.NoError(t, err)
	require.Equal(t, model.ManagedConfigModeAudit, binding.Mode)
	require.Equal(t, model.ManagedConfigDriftUnknown, binding.DriftStatus)
	require.NotEmpty(t, binding.DesiredHash)

	_, err = PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{IdempotencyKey: "audit-mode-plan-key", ActorID: 1})
	require.ErrorIs(t, err, ErrObserveModeWrite)
	require.ErrorIs(t, DeleteConfigTemplate(template.Id), ErrConfigTemplateInUse)
}

func TestConfigApplyVerifiesAndUpdatesBinding(t *testing.T) {
	state, server := newConfigOptionServer(t, false)
	defer server.Close()
	instance, template, preview := setupConfigApplyTest(t, server.URL)

	key := "config-success-idempotency"
	planned, err := PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{
		ExpectedObservedHash: preview.ObservedHash, IdempotencyKey: key, ActorID: 7,
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceActionApplyConfig, planned.Action)
	require.Equal(t, "medium", planned.RiskLevel)
	require.NotContains(t, planned.Parameters, "secret")
	_, _, err = ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 7,
		RejectedAction: model.ManagedInstanceActionApplyConfig,
	})
	require.ErrorIs(t, err, ErrOperationNotFound)

	queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 7})
	require.NoError(t, err)
	require.NotNil(t, task)
	completed, err := RunOperation(context.Background(), queued.OperationId, task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceOperationStatusSucceeded, completed.Status)

	state.mu.Lock()
	require.Equal(t, "Managed API", state.values["SystemName"])
	require.Equal(t, "https://example.com/new-logo.png", state.values["Logo"])
	state.mu.Unlock()
	binding, err := GetConfigBinding(instance.Id)
	require.NoError(t, err)
	require.Equal(t, template.Id, binding.TemplateId)
	require.Equal(t, model.ManagedConfigDriftInSync, binding.DriftStatus)
	require.NotZero(t, binding.LastAppliedAt)
	replayed, err := PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{
		ExpectedObservedHash: preview.ObservedHash, IdempotencyKey: key, ActorID: 7,
	})
	require.NoError(t, err)
	require.True(t, replayed.IdempotentReplay)
	require.Equal(t, planned.OperationId, replayed.OperationId)
}

func TestQueuedConfigApplyBlocksPolicyMutation(t *testing.T) {
	_, server := newConfigOptionServer(t, false)
	defer server.Close()
	instance, template, preview := setupConfigApplyTest(t, server.URL)
	key := "config-active-policy-key"
	planned, err := PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{
		ExpectedObservedHash: preview.ObservedHash, IdempotencyKey: key, ActorID: 7,
	})
	require.NoError(t, err)
	_, _, err = ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 7,
	})
	require.NoError(t, err)
	_, err = SetConfigBinding(instance.Id, ConfigBindingInput{
		TemplateID: template.Id, Mode: model.ManagedConfigModeAudit, ActorID: 7,
	})
	require.ErrorIs(t, err, ErrConfigOperationActive)
	_, err = UpdateConfigTemplate(template.Id, ConfigTemplateInput{
		Name: "Changed while queued", Kind: model.ManagedInstanceKindNewAPI, SchemaVersion: 1, ActorID: 7,
		Values: json.RawMessage(`{"ui.site_name":"Changed API"}`),
	})
	require.ErrorIs(t, err, ErrConfigOperationActive)
}

func TestDriftObservationDoesNotOverwriteReplacementBinding(t *testing.T) {
	_, server := newConfigOptionServer(t, false)
	defer server.Close()
	instance, _, _ := setupConfigApplyTest(t, server.URL)
	stale, err := GetConfigBinding(instance.Id)
	require.NoError(t, err)
	replacement, err := CreateConfigTemplate(ConfigTemplateInput{
		Name: "Replacement", Kind: model.ManagedInstanceKindNewAPI, SchemaVersion: 1, ActorID: 7,
		Values: json.RawMessage(`{"ui.site_name":"Replacement API"}`),
	})
	require.NoError(t, err)
	current, err := SetConfigBinding(instance.Id, ConfigBindingInput{
		TemplateID: replacement.Id, Mode: model.ManagedConfigModeAudit, ActorID: 7,
	})
	require.NoError(t, err)
	require.ErrorIs(t, updateConfigBindingObservation(stale, "stale-hash", model.ManagedConfigDriftInSync, ""), ErrConfigStateConflict)
	refreshed, err := GetConfigBinding(instance.Id)
	require.NoError(t, err)
	require.Equal(t, current.TemplateId, refreshed.TemplateId)
	require.Equal(t, model.ManagedConfigDriftUnknown, refreshed.DriftStatus)
}

func TestConfigApplyDoesNotCompensateUnknownWrite(t *testing.T) {
	state := &configOptionState{values: map[string]any{
		"SystemName": "Old API", "Logo": "https://example.com/old-logo.png",
	}}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		if request.Method == http.MethodGet {
			items := make([]map[string]any, 0, len(state.values))
			for key, value := range state.values {
				items = append(items, map[string]any{"key": key, "value": value})
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"success": true, "data": items})
			return
		}
		var body struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		state.values[body.Key] = body.Value
		writes++
		connection, _, err := response.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	instance, _, preview := setupConfigApplyTest(t, server.URL)
	key := "config-unknown-write-key"
	planned, err := PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{
		ExpectedObservedHash: preview.ObservedHash, IdempotencyKey: key, ActorID: 7,
	})
	require.NoError(t, err)
	queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 7,
	})
	require.NoError(t, err)
	completed, err := RunOperation(context.Background(), queued.OperationId, task.TaskID)
	require.Error(t, err)
	require.Equal(t, model.ManagedInstanceOperationStatusUnknown, completed.Status)
	require.Equal(t, 1, writes)
}

func TestConfigApplyPlanIsInvalidatedByBindingChange(t *testing.T) {
	_, server := newConfigOptionServer(t, false)
	defer server.Close()
	instance, _, preview := setupConfigApplyTest(t, server.URL)
	key := "config-stale-binding-key"
	planned, err := PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{
		ExpectedObservedHash: preview.ObservedHash, IdempotencyKey: key, ActorID: 9,
	})
	require.NoError(t, err)
	replacement, err := CreateConfigTemplate(ConfigTemplateInput{
		Name: "Replacement branding", Kind: model.ManagedInstanceKindNewAPI, SchemaVersion: 1, ActorID: 9,
		Values: json.RawMessage(`{"ui.site_name":"Replacement API"}`),
	})
	require.NoError(t, err)
	_, err = SetConfigBinding(instance.Id, ConfigBindingInput{TemplateID: replacement.Id, Mode: model.ManagedConfigModeEnforce, ActorID: 9})
	require.NoError(t, err)
	_, _, err = ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 9,
	})
	require.ErrorIs(t, err, ErrOperationNotExecutable)
}

func TestConfigApplyPlanIsInvalidatedByTemplateUpdate(t *testing.T) {
	_, server := newConfigOptionServer(t, false)
	defer server.Close()
	instance, template, preview := setupConfigApplyTest(t, server.URL)
	key := "config-stale-template-key"
	planned, err := PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{
		ExpectedObservedHash: preview.ObservedHash, IdempotencyKey: key, ActorID: 9,
	})
	require.NoError(t, err)
	_, err = UpdateConfigTemplate(template.Id, ConfigTemplateInput{
		Name: "Managed branding", Kind: model.ManagedInstanceKindNewAPI, SchemaVersion: 1, ActorID: 9,
		Values: json.RawMessage(`{"ui.site_name":"Changed API","ui.logo_url":"https://example.com/new-logo.png"}`),
	})
	require.NoError(t, err)
	_, _, err = ExecuteOperation(instance.Id, ExecuteOperationInput{
		OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 9,
	})
	require.ErrorIs(t, err, ErrOperationNotExecutable)
	binding, err := GetConfigBinding(instance.Id)
	require.NoError(t, err)
	require.Equal(t, model.ManagedConfigDriftUnknown, binding.DriftStatus)
}

func TestSub2ConfigWriteRequiresSuccessEnvelope(t *testing.T) {
	require.ErrorIs(t, requireSub2SettingsWrite(&ConnectorResponse{
		StatusCode: http.StatusOK, Body: []byte(`{"message":"success"}`),
	}), ErrRemoteConfigInvalid)
	require.ErrorIs(t, requireSub2SettingsWrite(&ConnectorResponse{
		StatusCode: http.StatusOK, Body: []byte(`{"code":0}`),
	}), ErrRemoteConfigInvalid)
	require.NoError(t, requireSub2SettingsWrite(&ConnectorResponse{
		StatusCode: http.StatusOK, Body: []byte(`{"code":0,"message":"success"}`),
	}))
}

func TestConfigApplyCompensatesPartialNewAPIWrite(t *testing.T) {
	state, server := newConfigOptionServer(t, true)
	defer server.Close()
	instance, _, preview := setupConfigApplyTest(t, server.URL)
	key := "config-compensation-key"
	planned, err := PlanConfigApply(context.Background(), instance.Id, PlanConfigApplyInput{
		ExpectedObservedHash: preview.ObservedHash, IdempotencyKey: key, ActorID: 8,
	})
	require.NoError(t, err)
	queued, task, err := ExecuteOperation(instance.Id, ExecuteOperationInput{OperationID: planned.OperationId, IdempotencyKey: key, ActorID: 8})
	require.NoError(t, err)
	completed, err := RunOperation(context.Background(), queued.OperationId, task.TaskID)
	require.Error(t, err)
	require.Equal(t, model.ManagedInstanceOperationStatusFailed, completed.Status)
	require.Equal(t, "config_apply_failed_rolled_back", completed.ErrorCode)

	state.mu.Lock()
	require.Equal(t, "Old API", state.values["SystemName"])
	require.Equal(t, "https://example.com/old-logo.png", state.values["Logo"])
	state.mu.Unlock()
	result := completed.Result.(map[string]any)
	require.Equal(t, true, result["compensated"])
}

type configOptionState struct {
	mu           sync.Mutex
	values       map[string]any
	failSiteName bool
}

func newConfigOptionServer(t *testing.T, failSiteName bool) (*configOptionState, *httptest.Server) {
	t.Helper()
	state := &configOptionState{values: map[string]any{
		"SystemName": "Old API", "Logo": "https://example.com/old-logo.png",
	}, failSiteName: failSiteName}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer config-secret", request.Header.Get("Authorization"))
		state.mu.Lock()
		defer state.mu.Unlock()
		switch request.Method {
		case http.MethodGet:
			items := make([]map[string]any, 0, len(state.values))
			for key, value := range state.values {
				items = append(items, map[string]any{"key": key, "value": value})
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"success": true, "data": items})
		case http.MethodPut:
			var body struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			if state.failSiteName && body.Key == "SystemName" && body.Value == "Managed API" {
				state.failSiteName = false
				response.WriteHeader(http.StatusInternalServerError)
				_, _ = response.Write([]byte(`{"success":false}`))
				return
			}
			state.values[body.Key] = body.Value
			_ = json.NewEncoder(response).Encode(map[string]any{"success": true})
		default:
			response.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	return state, server
}

func setupConfigApplyTest(t *testing.T, baseURL string) (*model.ManagedInstance, *ConfigTemplateView, *ConfigPreview) {
	t.Helper()
	setupManagedInstanceOperationTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	instance := createConfigTestInstance(t, baseURL, model.ManagedInstanceModeEnforce)
	credential, err := buildCredential(instance.Id, CredentialInput{AuthType: "bearer_pat", Secret: "config-secret"}, 7)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(credential).Error)
	require.NoError(t, model.DB.Model(instance).Update("capabilities", `["config.read","config.apply"]`).Error)
	instance.Capabilities = `["config.read","config.apply"]`
	template, err := CreateConfigTemplate(ConfigTemplateInput{
		Name: "Managed branding", Kind: model.ManagedInstanceKindNewAPI, SchemaVersion: 1, ActorID: 7,
		Values: json.RawMessage(`{"ui.site_name":"Managed API","ui.logo_url":"https://example.com/new-logo.png"}`),
	})
	require.NoError(t, err)
	_, err = SetConfigBinding(instance.Id, ConfigBindingInput{TemplateID: template.Id, Mode: model.ManagedConfigModeEnforce, ActorID: 7})
	require.NoError(t, err)
	preview, err := RefreshConfigPreview(context.Background(), instance.Id, 7)
	require.NoError(t, err)
	require.True(t, preview.Drifted)
	require.Len(t, preview.Differences, 2)
	return instance, template, preview
}

func createConfigTestInstance(t *testing.T, baseURL string, mode string) *model.ManagedInstance {
	t.Helper()
	instance := &model.ManagedInstance{
		Name: "config-" + idempotencyFingerprint(baseURL), Kind: model.ManagedInstanceKindNewAPI,
		BaseURL: baseURL, Environment: "staging", ManagementMode: mode, TLSVerify: true,
		RequestTimeoutSeconds: 5, CheckIntervalSeconds: 60, Capabilities: `[]`,
	}
	require.NoError(t, model.DB.Create(instance).Error)
	return instance
}
