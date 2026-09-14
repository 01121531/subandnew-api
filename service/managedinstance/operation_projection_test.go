package managedinstance

import (
	"encoding/json"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
)

func projectedManagedDTO(t *testing.T, access *authz.DataAccess, value any) (map[string]any, string) {
	t.Helper()
	projected, err := ProjectManagedInstanceDTO(access, value)
	require.NoError(t, err)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, json.Unmarshal(encoded, &object))
	return object, string(encoded)
}

func TestManagedDTOProjectionPreservesOperationAndBatchStructure(t *testing.T) {
	access := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	op := &OperationView{ManagedInstanceOperation: &model.ManagedInstanceOperation{
		OperationId: "operation-public", InstanceId: 1, ActorId: 1, Status: "planned", Action: model.ManagedInstanceActionToggleResource,
		RiskLevel: "high", WritesRemote: true, RequiredCapability: "channels.toggle", CreatedAt: 123,
		IdempotencyKey: "private-key", ErrorCode: `{"email":"private-email"}`,
	}, Parameters: toggleResourceParameters{ResourceID: 7, Enabled: boolPointer(true)},
		Plan: operationPlan{Action: model.ManagedInstanceActionToggleResource, RiskLevel: "high", WritesRemote: true,
			RequiredCapability: "channels.toggle", TargetCount: 1, Summary: "private-email", ExpectedETag: "private-etag"},
		Result: remoteOperationResult{Action: model.ManagedInstanceActionToggleResource, ResourceKind: "channel", Count: 1,
			Items: []remoteResultItem{{ResourceID: 7, Succeeded: true, Enabled: boolPointer(true)}}}, IdempotentReplay: true}
	object, encoded := projectedManagedDTO(t, access, op)
	require.Equal(t, "planned", object["status"])
	require.Equal(t, true, object["idempotent_replay"])
	require.Equal(t, float64(7), object["parameters"].(map[string]any)["resource_id"])
	require.Equal(t, "channels.toggle", object["plan"].(map[string]any)["required_capability"])
	for _, hidden := range []string{"private", "created_at", "target_count", "enabled", "succeeded", `"count"`} {
		require.NotContains(t, encoded, hidden)
	}
	batch := &BatchOperationView{ManagedInstanceOperationBatch: &model.ManagedInstanceOperationBatch{
		BatchId: "batch-public", Status: "queued", TargetCount: 1}, Summary: BatchOperationSummary{Total: 1, Planned: 1},
		Items: []BatchOperationItemView{{InstanceID: 1, Position: 0, Status: "planned", Parameters: op.Parameters, Operation: op}}}
	object, encoded = projectedManagedDTO(t, access, batch)
	require.Empty(t, object["summary"])
	require.Len(t, object["items"], 1)
	require.NotContains(t, encoded, "private")
	access.Policy.Fields = map[string]bool{"accounts": true, "status": true, "time": true}
	object, _ = projectedManagedDTO(t, access, op)
	require.Equal(t, float64(123), object["created_at"])
	require.Equal(t, true, object["parameters"].(map[string]any)["enabled"])
	require.Equal(t, true, object["result"].(map[string]any)["items"].([]any)[0].(map[string]any)["succeeded"])
	object, _ = projectedManagedDTO(t, access, batch)
	require.Equal(t, float64(1), object["summary"].(map[string]any)["planned"])
}

func TestManagedDTOProjectionFunctionGrantCannotRevealConfigBeforeAfter(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	operationAccessTestActor(t, db, 1)
	access, err := authz.LoadDataAccess(db, 200)
	require.NoError(t, err)
	require.True(t, authz.Can(200, access.Role, authz.ManagedTemplateApply))
	require.False(t, access.HasField("email"))
	values := map[string]any{"ui.site_name": "Public site", "email": "private-before", "amount": 99887, "secret": "private-secret"}
	template := &ConfigTemplateView{ManagedConfigTemplate: &model.ManagedConfigTemplate{Id: 3, Name: "Public template",
		Kind: model.ManagedInstanceKindNewAPI, SchemaVersion: 1, CreatedBy: 1, Description: "private-description"}, Values: values}
	binding := &ConfigBindingView{ManagedInstanceConfigBinding: &model.ManagedInstanceConfigBinding{
		InstanceId: 1, TemplateId: 3, Mode: model.ManagedConfigModeEnforce, DriftStatus: model.ManagedConfigDriftDrifted, LastObservedHash: "private-hash"}, Template: template}
	differences := []ConfigDiff{{Key: "ui.site_name", Current: "Old site", Desired: "Public site"},
		{Key: "email", Current: "private-before", Desired: "private-after"},
		{Key: "ui.logo_url", Current: map[string]any{"email": "private-nested"}, Desired: `{"email":"private-string"}`}}
	preview := &ConfigPreview{Binding: binding, Observed: values, Desired: values, Differences: differences,
		ObservedHash: "private-hash", DesiredHash: "private-hash", Drifted: true, ObservedAt: 123}
	object, encoded := projectedManagedDTO(t, access, preview)
	require.Equal(t, "Public site", object["desired"].(map[string]any)["ui.site_name"])
	require.Len(t, object["differences"], 1)
	require.Equal(t, "Old site", object["differences"].([]any)[0].(map[string]any)["current"])
	require.NotContains(t, encoded, "private")
	require.NotContains(t, encoded, "99887")
	op := &OperationView{ManagedInstanceOperation: &model.ManagedInstanceOperation{ActorId: 1, Action: model.ManagedInstanceActionApplyConfig},
		Parameters: applyConfigParameters{TemplateID: 3, SchemaVersion: 1, ExpectedHash: "private-hash", Desired: values, Rollback: values},
		Plan: map[string]any{"action": "apply_config", "target_count": 3, "differences": differences,
			"before": `{"email":"private-before"}`, "after": "private-after"},
		Result: remoteOperationResult{ChangedFields: []string{"ui.site_name", "email"}, DesiredHash: "private-hash"}}
	object, encoded = projectedManagedDTO(t, access, op)
	require.Equal(t, "Public site", object["parameters"].(map[string]any)["rollback"].(map[string]any)["ui.site_name"])
	require.NotContains(t, encoded, "private")
	require.NotContains(t, encoded, "email")
	access.Policy.Fields["accounts"], access.Policy.Fields["status"] = true, true
	object, _ = projectedManagedDTO(t, access, op)
	require.Equal(t, float64(1), object["plan"].(map[string]any)["target_count"])
	object, _ = projectedManagedDTO(t, access, preview)
	require.Equal(t, true, object["drifted"])
	require.NotContains(t, object["binding"].(map[string]any), "drift_status")
	preview.Differences = differences[1:]
	object, _ = projectedManagedDTO(t, access, preview)
	require.Equal(t, false, object["drifted"])
	object, encoded = projectedManagedDTO(t, access, &ConfigTemplateList{Items: []*ConfigTemplateView{template}})
	require.Len(t, object["items"], 1)
	require.NotContains(t, encoded, "private")
}

func TestManagedDTOProjectionFilterRuleValuesRequireTheirField(t *testing.T) {
	access := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.FullAdminDataPolicy()}
	access.Policy.Fields["email"] = false
	template := &AccountFilterTemplateView{Id: 1, Name: "Saved filter", MatchMode: AccountFilterMatchAll,
		Rules: []AccountFilterRule{{Field: "email", Operator: "is", Values: []string{"private-email"}, ValueMode: AccountFilterValueAny}}}
	projected, err := ProjectManagedInstanceDTO(access, []*AccountFilterTemplateView{template})
	require.NoError(t, err)
	require.Empty(t, projected)
	access.Policy.Fields["email"] = true
	object, encoded := projectedManagedDTO(t, access, template)
	require.Equal(t, AccountFilterMatchAll, object["match_mode"])
	require.Contains(t, encoded, "private-email")
	for _, field := range []string{"email", "amount", "tokens", "rpm", "active_sessions", "utilization_5h", "vendor_email", "group", "status", "created_at", "future_private"} {
		template.Rules[0].Field = field
		access.Policy = model.EmptyAdminDataPolicy()
		projected, err = ProjectManagedInstanceDTO(access, template)
		require.NoError(t, err)
		require.Nil(t, projected, field)
	}
	template.Rules[0].Field = "name"
	template.Rules[0].Values = []string{`{"email":"private-email"}`}
	projected, err = ProjectManagedInstanceDTO(access, template)
	require.NoError(t, err)
	require.Nil(t, projected)
}

func TestManagedDTOProjectionTaskStringsAndNestedResultsFailClosed(t *testing.T) {
	access := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	for _, raw := range []string{`"{\"email\":\"private-email\"}"`, `"private-opaque"`, `{"email":"private-email","result":"private-nested"}`, `invalid-private-json`} {
		task := &model.SystemTask{TaskID: "public-task", Type: model.SystemTaskTypeManagedInstanceOperation, Status: model.SystemTaskStatusRunning,
			Payload: raw, State: raw, Result: raw, Error: "private-error", LockedBy: "private-worker", ScopeKey: "private-scope"}
		object, encoded := projectedManagedDTO(t, access, task.ToResponse())
		require.Equal(t, "running", object["status"])
		require.Equal(t, "public-task", object["task_id"])
		require.NotContains(t, encoded, "private")
		require.Empty(t, object["payload"])
	}
	op := &OperationView{ManagedInstanceOperation: &model.ManagedInstanceOperation{OperationId: "public-operation", ActorId: 1},
		Parameters: `{"email":"private-parameters"}`, Plan: `{"email":"private-plan"}`, Result: `{"email":"private-result"}`}
	encoded, err := json.Marshal(op)
	require.NoError(t, err)
	task := (&model.SystemTask{TaskID: "public-task", Type: model.SystemTaskTypeManagedInstanceOperation,
		Payload: `{"operation_id":"public-operation","actor_id":"private-actor","instance_id":1,"secret":"private-secret"}`,
		Result:  string(encoded)}).ToResponse()
	object, output := projectedManagedDTO(t, access, OperationTaskView{Operation: op, Task: &task})
	require.Contains(t, output, "public-operation")
	require.NotContains(t, output, "private")
	require.Equal(t, float64(1), object["task"].(map[string]any)["payload"].(map[string]any)["instance_id"])
}

func TestManagedDTOProjectionSchemasAndUnknownTypes(t *testing.T) {
	access := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	projected, err := ProjectManagedInstanceDTO(access, ListConfigSchemas())
	require.NoError(t, err)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	for _, key := range []string{"ui.site_name", "remote_key", "max_length", "enum", "ui.table_page_size"} {
		require.Contains(t, string(encoded), key)
	}
	_, err = ProjectManagedInstanceDTO(access, map[string]any{"values": "private"})
	require.ErrorIs(t, err, authz.ErrDataForbidden)
	var absent *ConfigBindingView
	projected, err = ProjectManagedInstanceDTO(access, absent)
	require.NoError(t, err)
	require.Nil(t, projected)
}

func TestManagedDTOProjectionProbeTaskKeepsOnlyGrantedResultFields(t *testing.T) {
	access := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	result, err := json.Marshal(&ProbeResult{Kind: model.ManagedInstanceKindNewAPI, Status: "ok", CheckedAt: 123,
		LatencyMS: 12, SystemName: "private-remote-text", Version: "private-version"})
	require.NoError(t, err)
	task := (&model.SystemTask{TaskID: "public-task", Type: model.SystemTaskTypeManagedInstanceProbe,
		Status: model.SystemTaskStatusSucceeded, Result: string(result)}).ToResponse()
	object, encoded := projectedManagedDTO(t, access, task)
	require.Equal(t, "succeeded", object["status"])
	require.Equal(t, map[string]any{"kind": model.ManagedInstanceKindNewAPI}, object["result"])
	require.NotContains(t, encoded, "private")
	access.Policy.Fields["status"], access.Policy.Fields["time"] = true, true
	object, encoded = projectedManagedDTO(t, access, task)
	require.Equal(t, "ok", object["result"].(map[string]any)["status"])
	require.Equal(t, float64(123), object["result"].(map[string]any)["checked_at"])
	require.NotContains(t, encoded, "private")
}
