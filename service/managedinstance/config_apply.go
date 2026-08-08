package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

var ErrConfigAlreadyInSync = errors.New("managed instance config is already in sync")

type PlanConfigApplyInput struct {
	ExpectedObservedHash string
	IdempotencyKey       string
	ActorID              int
}

func PlanConfigApply(ctx context.Context, instanceID int64, input PlanConfigApplyInput) (*OperationView, error) {
	input.ExpectedObservedHash = strings.TrimSpace(input.ExpectedObservedHash)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if instanceID <= 0 || input.ActorID <= 0 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return nil, ErrInvalidOperation
	}
	binding, err := GetConfigBinding(instanceID)
	if err != nil {
		return nil, err
	}
	if binding.Mode != model.ManagedConfigModeEnforce {
		return nil, ErrObserveModeWrite
	}
	existing, err := findOperationByIdempotency(instanceID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Action != model.ManagedInstanceActionApplyConfig {
			return nil, ErrIdempotencyConflict
		}
		var existingParameters applyConfigParameters
		if json.Unmarshal([]byte(existing.Parameters), &existingParameters) != nil ||
			existingParameters.TemplateID != binding.TemplateId ||
			(input.ExpectedObservedHash != "" && input.ExpectedObservedHash != existingParameters.ExpectedHash) {
			return nil, ErrIdempotencyConflict
		}
		view := operationView(existing)
		view.IdempotentReplay = true
		return view, nil
	}
	instance, err := getOperationInstance(instanceID)
	if err != nil {
		return nil, err
	}
	definition, err := operationDefinitionFor(instance.Kind, model.ManagedInstanceActionApplyConfig)
	if err != nil {
		return nil, err
	}
	if err := authorizeOperation(instance, definition); err != nil {
		return nil, err
	}
	preview, err := RefreshConfigPreview(ctx, instanceID, input.ActorID)
	if err != nil {
		return nil, err
	}
	if input.ExpectedObservedHash != "" && input.ExpectedObservedHash != preview.ObservedHash {
		return nil, ErrConfigStateConflict
	}
	if !preview.Drifted {
		return nil, ErrConfigAlreadyInSync
	}
	parameters := applyConfigParameters{
		TemplateID: binding.TemplateId, SchemaVersion: binding.Template.SchemaVersion,
		ExpectedHash: preview.ObservedHash, Desired: preview.Desired, Rollback: preview.Observed,
	}
	parametersJSON, _ := json.Marshal(parameters)
	plan := operationPlan{
		Action: model.ManagedInstanceActionApplyConfig, RiskLevel: "medium", WritesRemote: true,
		RequiredCapability: definition.capability, TargetCount: len(preview.Differences),
		Summary:            fmt.Sprintf("Apply %d whitelisted configuration changes", len(preview.Differences)),
		ExpectedConfigHash: preview.ObservedHash, TemplateID: binding.TemplateId, Differences: preview.Differences,
	}
	planJSON, _ := json.Marshal(plan)
	planHash := operationPlanHash(model.ManagedInstanceActionApplyConfig, parametersJSON)
	operationID, err := generateOperationID()
	if err != nil {
		return nil, err
	}
	operation := &model.ManagedInstanceOperation{
		OperationId: operationID, InstanceId: instanceID, ActorId: input.ActorID,
		Action: model.ManagedInstanceActionApplyConfig, Status: model.ManagedInstanceOperationStatusPlanned,
		RiskLevel: "medium", WritesRemote: true, RequiredCapability: definition.capability,
		IdempotencyKey: idempotencyDigest(input.IdempotencyKey), IdempotencyFingerprint: idempotencyFingerprint(input.IdempotencyKey),
		PlanHash: planHash, Parameters: string(parametersJSON), Plan: string(planJSON),
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(operation).Error; err != nil {
			return err
		}
		return writeAuditOutcome(tx, instanceID, input.ActorID, "config_apply_plan", "succeeded", map[string]any{
			"operation_id": operationID, "template_id": binding.TemplateId,
			"difference_count": len(preview.Differences), "idempotency_fingerprint": operation.IdempotencyFingerprint,
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

func executeConfigApply(ctx context.Context, connector *Connector, instance *model.ManagedInstance, headers http.Header, raw json.RawMessage) (*remoteOperationResult, error) {
	if connector == nil || instance == nil {
		return nil, ErrInvalidOperation
	}
	var params applyConfigParameters
	if err := decodeStrictJSON(raw, &params); err != nil || params.TemplateID <= 0 || params.SchemaVersion <= 0 || len(params.ExpectedHash) != 64 || len(params.Desired) == 0 || len(params.Rollback) == 0 {
		return nil, ErrInvalidOperation
	}
	desired, err := validateConfigValueMap(instance.Kind, params.SchemaVersion, params.Desired)
	if err != nil {
		return nil, err
	}
	rollback, err := validateConfigValueMap(instance.Kind, params.SchemaVersion, params.Rollback)
	if err != nil {
		return nil, err
	}
	current, err := readRemoteConfigWithConnector(ctx, connector, instance.Kind, headers, desired)
	if err != nil {
		return nil, err
	}
	currentHash, _ := configValuesHash(current)
	if currentHash != params.ExpectedHash {
		return nil, ErrConfigStateConflict
	}
	changed := changedConfigKeys(current, desired)
	desiredHash, _ := configValuesHash(desired)
	if len(changed) == 0 {
		return &remoteOperationResult{Action: model.ManagedInstanceActionApplyConfig, ResourceKind: "config", ObservedHash: currentHash, DesiredHash: desiredHash, Verified: true}, nil
	}
	written, applyErr := writeRemoteConfig(ctx, connector, instance.Kind, headers, desired, changed)
	if applyErr == nil {
		verified, verifyErr := readRemoteConfigWithConnector(ctx, connector, instance.Kind, headers, desired)
		applyErr = verifyErr
		if applyErr == nil {
			verifiedHash, _ := configValuesHash(verified)
			if verifiedHash == desiredHash {
				return &remoteOperationResult{
					Action: model.ManagedInstanceActionApplyConfig, ResourceKind: "config", Count: len(changed),
					ChangedFields: changed, ObservedHash: verifiedHash, DesiredHash: desiredHash, Verified: true,
				}, nil
			}
			applyErr = ErrConfigStateConflict
		}
	}
	if len(written) == 0 {
		return nil, applyErr
	}
	result := &remoteOperationResult{
		Action: model.ManagedInstanceActionApplyConfig, ResourceKind: "config", Count: len(written),
		ChangedFields: written,
	}
	var unknownWrite *remoteConfigWriteUnknownError
	if errors.As(applyErr, &unknownWrite) {
		return result, &ConfigApplyError{Code: "config_write_result_unknown", Result: result, Unknown: true}
	}
	compensationState, readErr := readRemoteConfigWithConnector(ctx, connector, instance.Kind, headers, desired)
	if readErr != nil {
		return result, &ConfigApplyError{Code: "config_compensation_state_unknown", Result: result, Unknown: true}
	}
	rollbackKeys := make([]string, 0, len(written))
	for _, key := range written {
		switch {
		case configValuesEqual(compensationState[key], desired[key]):
			rollbackKeys = append(rollbackKeys, key)
		case configValuesEqual(compensationState[key], rollback[key]):
		default:
			return result, &ConfigApplyError{Code: "config_compensation_conflict", Result: result, Unknown: true}
		}
	}
	if instance.Kind != model.ManagedInstanceKindSub2API {
		for left, right := 0, len(rollbackKeys)-1; left < right; left, right = left+1, right-1 {
			rollbackKeys[left], rollbackKeys[right] = rollbackKeys[right], rollbackKeys[left]
		}
	}
	var compensationErr error
	if len(rollbackKeys) > 0 {
		_, compensationErr = writeRemoteConfig(ctx, connector, instance.Kind, headers, rollback, rollbackKeys)
	}
	if compensationErr == nil {
		rolledBack, readErr := readRemoteConfigWithConnector(ctx, connector, instance.Kind, headers, rollback)
		compensationErr = readErr
		if compensationErr == nil {
			rollbackHash, _ := configValuesHash(rollback)
			observedHash, _ := configValuesHash(rolledBack)
			if rollbackHash != observedHash {
				compensationErr = ErrConfigStateConflict
			}
		}
	}
	result.Compensated = compensationErr == nil
	if compensationErr == nil {
		return result, &ConfigApplyError{Code: "config_apply_failed_rolled_back", Compensated: true, Result: result}
	}
	return result, &ConfigApplyError{Code: "config_compensation_failed", Result: result, Unknown: true}
}

func validateConfigValueMap(kind string, schemaVersion int, values map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return validateConfigValues(kind, schemaVersion, encoded)
}

func changedConfigKeys(current map[string]any, desired map[string]any) []string {
	keys := make([]string, 0)
	for key, desiredValue := range desired {
		if !configValuesEqual(current[key], desiredValue) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func writeRemoteConfig(ctx context.Context, connector *Connector, kind string, headers http.Header, values map[string]any, keys []string) ([]string, error) {
	schema, err := ConfigSchemaForKind(kind)
	if err != nil {
		return nil, err
	}
	fields := configFieldMap(schema)
	keys = append([]string(nil), keys...)
	switch kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		written := make([]string, 0, len(keys))
		for _, key := range keys {
			field, ok := fields[key]
			if !ok {
				return written, ErrInvalidConfigTemplate
			}
			response, wrote, err := doTrackedConfigWrite(ctx, connector, http.MethodPut, "/api/option/", headers, map[string]any{"key": field.RemoteKey, "value": values[key]})
			if err != nil {
				if wrote {
					written = append(written, key)
					return written, &remoteConfigWriteUnknownError{cause: err}
				}
				return written, err
			}
			if err := requireNewAPISuccess(response); err != nil {
				return append(written, key), err
			}
			written = append(written, key)
		}
		return written, nil
	case model.ManagedInstanceKindSub2API:
		payload := make(map[string]any, len(keys))
		for _, key := range keys {
			field, ok := fields[key]
			if !ok {
				return nil, ErrInvalidConfigTemplate
			}
			payload[field.RemoteKey] = values[key]
		}
		response, wrote, err := doTrackedConfigWrite(ctx, connector, http.MethodPut, "/api/v1/admin/settings", headers, payload)
		if err != nil {
			if wrote {
				return keys, &remoteConfigWriteUnknownError{cause: err}
			}
			return nil, err
		}
		if err := requireSub2SettingsWrite(response); err != nil {
			return keys, err
		}
		return keys, nil
	default:
		return nil, ErrUnsupportedCapability
	}
}

type remoteConfigWriteUnknownError struct{ cause error }

func (err *remoteConfigWriteUnknownError) Error() string {
	return "remote config write result is unknown: " + err.cause.Error()
}

func (err *remoteConfigWriteUnknownError) Unwrap() error { return err.cause }

func doTrackedConfigWrite(ctx context.Context, connector *Connector, method string, path string, headers http.Header, body any) (*ConnectorResponse, bool, error) {
	var wrote atomic.Bool
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) },
	})
	response, err := connector.DoJSON(traced, method, path, headers, body)
	return response, wrote.Load(), err
}

func authorizeConfigApplyBinding(instanceID int64, raw string) error {
	var params applyConfigParameters
	if json.Unmarshal([]byte(raw), &params) != nil || params.TemplateID <= 0 {
		return ErrOperationNotExecutable
	}
	binding, err := GetConfigBinding(instanceID)
	if err != nil {
		return ErrOperationNotExecutable
	}
	plannedHash, hashErr := configValuesHash(params.Desired)
	currentHash, currentHashErr := configValuesHash(binding.Template.Values)
	if hashErr != nil || currentHashErr != nil || binding.Mode != model.ManagedConfigModeEnforce ||
		binding.TemplateId != params.TemplateID || binding.Template.SchemaVersion != params.SchemaVersion ||
		plannedHash != currentHash || binding.DesiredHash != currentHash {
		return ErrOperationNotExecutable
	}
	return nil
}

func requireSub2SettingsWrite(response *ConnectorResponse) error {
	if err := requireHTTPStatus(response); err != nil {
		return err
	}
	var envelope map[string]any
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return ErrRemoteConfigInvalid
	}
	code, exists := envelope["code"]
	message, messageExists := envelope["message"].(string)
	if !exists || !messageExists || strings.TrimSpace(message) == "" || !sub2SuccessCode(code) {
		return ErrRemoteConfigInvalid
	}
	return nil
}

func markConfigApplyCompleted(tx *gorm.DB, instanceID int64, parameters string, result json.RawMessage) error {
	var params applyConfigParameters
	if err := json.Unmarshal([]byte(parameters), &params); err != nil {
		return ErrInvalidOperation
	}
	var remoteResult remoteOperationResult
	if err := json.Unmarshal(result, &remoteResult); err != nil || !remoteResult.Verified {
		return ErrRemoteConfigInvalid
	}
	now := common.GetTimestamp()
	return tx.Model(&model.ManagedInstanceConfigBinding{}).
		Where("instance_id = ? AND template_id = ? AND mode = ? AND desired_hash = ?", instanceID, params.TemplateID, model.ManagedConfigModeEnforce, remoteResult.DesiredHash).
		Updates(map[string]any{
			"drift_status": model.ManagedConfigDriftInSync, "last_observed_hash": remoteResult.ObservedHash,
			"desired_hash": remoteResult.DesiredHash, "last_error_code": "", "last_checked_at": now,
			"last_applied_at": now, "updated_at": now,
		}).Error
}
