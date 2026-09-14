package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func rbacContractDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.ManagedInstance{}))
	return db
}

func rbacContractUser(t *testing.T, db *gorm.DB) model.User {
	t.Helper()
	user := model.User{Username: "rbac-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func TestRBACContractMissingPolicyAndInstanceScope(t *testing.T) {
	db := rbacContractDB(t)
	user := rbacContractUser(t, db)
	for _, id := range []int64{1, 2, 3} {
		require.NoError(t, db.Create(&model.ManagedInstance{Id: id, Name: fmt.Sprintf("instance-%d", id), Kind: model.ManagedInstanceKindGeneric, BaseURL: fmt.Sprintf("https://instance-%d.example.test", id)}).Error)
	}
	access, err := LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	require.Equal(t, model.EmptyAdminDataPolicy(), access.Policy)
	for _, field := range model.AdminDataFields {
		assert.False(t, access.HasField(field), field)
	}
	for _, tc := range []struct {
		name  string
		scope string
		ids   []int64
		want  []int64
	}{
		{"selected-empty", "selected", []int64{}, []int64{}},
		{"selected-explicit", "selected", []int64{2}, []int64{2}},
		{"all", "all", nil, []int64{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access.Policy.InstanceScope, access.Policy.InstanceIDs = tc.scope, tc.ids
			var ids []int64
			require.NoError(t, access.ScopeQuery(db.Model(&model.ManagedInstance{}), "id").Order("id").Pluck("id", &ids).Error)
			assert.Equal(t, tc.want, ids)
			var count int64
			require.NoError(t, access.ScopeQuery(db.Model(&model.ManagedInstance{}), "id").Count(&count).Error)
			assert.EqualValues(t, len(tc.want), count)
			for _, id := range []int64{-1, 0, 1, 2, 3} {
				allowed := id > 0 && (tc.scope == "all" || id == 2 && len(tc.ids) > 0)
				assert.Equal(t, allowed, access.HasInstance(id))
				if allowed {
					assert.NoError(t, access.CheckInstances([]int64{id}))
				} else {
					assert.ErrorIs(t, access.CheckInstances([]int64{id}), ErrDataForbidden)
				}
			}
		})
	}
	access.Policy.InstanceScope, access.Policy.InstanceIDs = "selected", []int64{2}
	assert.ErrorIs(t, access.CheckInstances([]int64{2, 3}), ErrDataForbidden, "explicit mixed batches must not silently drop forbidden IDs")
}

func TestRBACContractPolicyRevisionAndOmittedUpdate(t *testing.T) {
	db := rbacContractDB(t)
	user := rbacContractUser(t, db)
	save := func(policy *model.AdminDataPolicy, create bool) error {
		return db.Transaction(func(tx *gorm.DB) error { return SaveDataPolicy(tx, user.Id, policy, create) })
	}
	require.NoError(t, save(nil, true))
	initial, err := LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, initial.Policy.Revision)
	assert.EqualValues(t, user.AuthorizationVersion+1, initial.Version)
	assert.False(t, initial.HasInstance(1))
	require.NoError(t, save(nil, false))
	require.NoError(t, save(&initial.Policy, false))
	require.NoError(t, initial.Current(db), "omission and identical saves must not revoke an unchanged policy")
	assert.ErrorIs(t, save(nil, true), ErrAuthorizationChanged)
	grant := model.FullAdminDataPolicy()
	grant.Revision = initial.Policy.Revision
	require.NoError(t, save(&grant, false))
	latest, err := LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	assert.Equal(t, initial.Policy.Revision+1, latest.Policy.Revision)
	assert.Equal(t, initial.Version+1, latest.Version)
	assert.ErrorIs(t, initial.Current(db), ErrAuthorizationChanged)
	assert.ErrorIs(t, save(&initial.Policy, false), ErrAuthorizationChanged)
	afterConflict, err := LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	assert.Equal(t, latest, afterConflict, "stale saves must not modify policy or authorization version")
}

func TestRBACContractCurrentRevocation(t *testing.T) {
	for _, mutation := range []string{"version", "revision", "policy-delete", "disabled", "demoted", "deleted"} {
		t.Run(mutation, func(t *testing.T) {
			db := rbacContractDB(t)
			user := rbacContractUser(t, db)
			policy := model.FullAdminDataPolicy()
			policy.UserID = user.Id
			require.NoError(t, db.Create(&policy).Error)
			access, err := LoadDataAccess(db, user.Id)
			require.NoError(t, err)
			require.NoError(t, access.Current(db))
			previous := model.DB
			model.DB = db
			t.Cleanup(func() { model.DB = previous })
			ctx := WithDataAccess(context.Background(), access)
			require.Same(t, access, DataAccessFrom(ctx))
			require.NoError(t, CheckContextInstances(ctx, 1))
			switch mutation {
			case "version":
				err = BumpAuthorizationVersion(db, user.Id)
			case "revision":
				err = db.Model(&policy).UpdateColumn("revision", policy.Revision+1).Error
			case "policy-delete":
				err = db.Delete(&policy).Error
			case "disabled":
				err = db.Model(&user).UpdateColumn("status", common.UserStatusDisabled).Error
			case "demoted":
				err = db.Model(&user).UpdateColumn("role", common.RoleCommonUser).Error
			case "deleted":
				err = db.Delete(&user).Error
			}
			require.NoError(t, err)
			assert.ErrorIs(t, access.Current(db), ErrAuthorizationChanged)
			assert.ErrorIs(t, CheckContextInstances(ctx, 1), ErrAuthorizationChanged)
			assert.NoError(t, CheckContextInstances(context.Background(), 1), "scheduled collectors have no user principal")
		})
	}
}

func TestRBACContractRootAndInvalidPrincipals(t *testing.T) {
	db := rbacContractDB(t)
	user := rbacContractUser(t, db)
	require.NoError(t, db.Model(&user).UpdateColumn("role", common.RoleRootUser).Error)
	access, err := LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	assert.True(t, access.AllFields())
	assert.True(t, access.HasInstance(999))
	require.NoError(t, db.Model(&user).UpdateColumn("status", common.UserStatusDisabled).Error)
	_, err = LoadDataAccess(db, user.Id)
	assert.ErrorIs(t, err, ErrDataForbidden)
	_, err = LoadDataAccess(db, user.Id+1)
	assert.ErrorIs(t, err, ErrDataForbidden)
}

func TestRBACContractProjectionOmissionAndCounts(t *testing.T) {
	access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	input := map[string]any{
		"items": []any{map[string]any{"id": int64(9007199254740993), "amount": 0, "requests": nil, "tokens": 0, "email": "private@example.test", "status": false, "created_at": 0, "future_secret": "secret"}},
		"total": 0, "total_pages": nil, "page": 1, "has_more": false,
		"summary": map[string]any{"count": 0, "available": 0, "enabled": 0, "amounts": map[string]float64{"USD": 0}},
	}
	projected, err := access.Project(input)
	require.NoError(t, err)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{"id":9007199254740993}],"page":1,"has_more":false,"summary":{}}`, string(encoded))
	assert.Contains(t, input["items"].([]any)[0], "email", "projection must not mutate its input")
	access.Policy.Fields = map[string]bool{"amount": true, "requests": true, "accounts": true, "status": true}
	projected, err = access.Project(input)
	require.NoError(t, err)
	encoded, err = json.Marshal(projected)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{"id":9007199254740993,"amount":0,"requests":null,"status":false}],"total":0,"total_pages":null,"page":1,"has_more":false,"summary":{"count":0,"available":0,"enabled":0,"amounts":{"USD":0}}}`, string(encoded))
	access.Policy.Fields = map[string]bool{"status": true}
	projected, err = access.Project(map[string]any{"items": []any{map[string]any{"available": false, "enabled": true}}, "summary": map[string]any{"available": 0, "enabled": 2}})
	require.NoError(t, err)
	encoded, err = json.Marshal(projected)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{"available":false,"enabled":true}],"summary":{}}`, string(encoded))
}

func TestRBACContractProjectionSensitiveAliases(t *testing.T) {
	aliases := map[string][]string{
		"amount":      {"actual_cost", "balance", "quota", "amount_usd", "currency", "costs_excluding_today"},
		"requests":    {"request_count", "successful_requests_24h", "total_calls"},
		"tokens":      {"prompt_tokens", "cache_read_input_tokens", "token_count"},
		"rpm":         {"rpm_capacity", "max_rpm", "capacity", "samples"},
		"concurrency": {"active_sessions", "max_sessions", "active_session_samples"},
		"accounts":    {"total", "total_rows", "total_pages", "matched_count", "processed", "added_accounts"},
		"rates":       {"utilization_5h", "success_rate_sample_count", "instance_availability"},
		"email":       {"email", "account_email", "user_email"},
		"vendor":      {"vendor_email", "owner_user_id", "ownership", "vendor_error_code"},
		"group":       {"group_ids", "user_group", "token_group"},
		"status":      {"health_status", "rate_limited"},
		"time":        {"created_at", "last_activity_at", "response_time_ms", "duration_ms", "elapsed_time"},
	}
	for group, keys := range aliases {
		for _, key := range keys {
			t.Run(group+"/"+key, func(t *testing.T) {
				access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
				assert.Equal(t, group, DataFieldGroup(key))
				assert.ErrorIs(t, access.CheckDataField(key), ErrDataForbidden)
				for _, value := range []any{nil, 0, "private"} {
					input := map[string]any{"items": []any{map[string]any{key: value}}, "filter_options": map[string]any{key: []any{value}}}
					projected, err := access.Project(input)
					require.NoError(t, err)
					encoded, err := json.Marshal(projected)
					require.NoError(t, err)
					assert.JSONEq(t, `{"items":[{}],"filter_options":{}}`, string(encoded))
				}
				if key == "vendor_email" {
					for _, singleGrant := range []string{"email", "vendor"} {
						t.Run(singleGrant+"-only", func(t *testing.T) {
							access.Policy.Fields = map[string]bool{singleGrant: true}
							assert.ErrorIs(t, access.CheckDataField(key), ErrDataForbidden)
							for _, value := range []any{nil, 0, "private@example.test"} {
								projected, err := access.Project(map[string]any{"items": []any{map[string]any{key: value}}, "filter_options": map[string]any{key: []any{value}}})
								require.NoError(t, err)
								encoded, err := json.Marshal(projected)
								require.NoError(t, err)
								assert.JSONEq(t, `{"items":[{}],"filter_options":{}}`, string(encoded))
							}
						})
					}
				}
				access.Policy.Fields = map[string]bool{group: true}
				for _, dependency := range dataFieldDependencies[key] {
					access.Policy.Fields[dependency] = true
				}
				assert.NoError(t, access.CheckDataField(key))
				projected, err := access.Project(map[string]any{key: nil})
				require.NoError(t, err)
				assert.Contains(t, projected, key, "granted null values must remain present")
				if key == "vendor_email" {
					projected, err := access.Project(map[string]any{key: "visible@example.test"})
					require.NoError(t, err)
					assert.Equal(t, "visible@example.test", projected.(map[string]any)[key])
				}
			})
		}
	}
	access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	assert.ErrorIs(t, access.CheckDataField("future_secret"), ErrDataForbidden)
}

func TestRBACContractProjectionLastAttemptRequiresTime(t *testing.T) {
	// managedaccount.SourceStatus exposes last_attempt_at alongside observed_at.
	access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	assert.ErrorIs(t, access.CheckDataField("last_attempt_at"), ErrDataForbidden)
	projected, err := access.Project(map[string]any{"sources": []any{map[string]any{"instance_id": 1, "last_attempt_at": 1720000000, "observed_at": 1720000000}}})
	require.NoError(t, err)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	assert.JSONEq(t, `{"sources":[{"instance_id":1}]}`, string(encoded))
}

func TestRBACContractProjectionUsageAliasesRetainGrantedValues(t *testing.T) {
	// These fields are emitted by conductorUsageRow or consumed by usage-records/index.tsx.
	for _, tc := range []struct {
		group string
		key   string
	}{
		{"tokens", "cache_read_tokens"},
		{"tokens", "cache_creation_tokens"},
		{"tokens", "cache_5m_tokens"},
		{"tokens", "cache_1h_tokens"},
		{"time", "use_time"},
		{"amount", "account_stats_cost"},
		{"amount", "account_rate_multiplier"},
	} {
		t.Run(tc.group+"/"+tc.key, func(t *testing.T) {
			access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
			input := map[string]any{tc.key: 12}
			denied, err := access.Project(input)
			require.NoError(t, err)
			assert.NotContains(t, denied, tc.key)
			access.Policy.Fields[tc.group] = true
			assert.NoError(t, access.CheckDataField(tc.key))
			allowed, err := access.Project(input)
			require.NoError(t, err)
			require.Contains(t, allowed, tc.key, "a partial grant must retain the actual DTO field")
			assert.Equal(t, json.Number("12"), allowed.(map[string]any)[tc.key])
		})
	}
}

func TestRBACContractProjectionRateSummaryRequiresRates(t *testing.T) {
	// The registry recognizes this wrapper and value; neither may bypass rates.
	access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	input := map[string]any{"success_rate_summary": map[string]any{"value": 0.95, "sample_count": 40}}
	projected, err := access.Project(input)
	require.NoError(t, err)
	assert.NotContains(t, projected, "success_rate_summary")
	access.Policy.Fields["rates"] = true
	projected, err = access.Project(input)
	require.NoError(t, err)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	assert.JSONEq(t, `{"success_rate_summary":{"value":0.95,"sample_count":40}}`, string(encoded))
}

func TestRBACContractRawUsageRowsDoNotExposeFreeText(t *testing.T) {
	// UsageRecordPage.Items in managedinstance/usage_records.go contains raw upstream JSON.
	for _, key := range []string{"message", "error", "status_message", "error_message", "last_error"} {
		t.Run(key, func(t *testing.T) {
			row, err := json.Marshal(map[string]any{"id": 1, "email": "private@example.test", key: "private@example.test secret=synthetic"})
			require.NoError(t, err)
			input := map[string]any{"items": []json.RawMessage{row}}
			access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
			access.Policy.Fields["status"] = true
			projected, err := access.Project(input)
			require.NoError(t, err)
			encoded, err := json.Marshal(projected)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "private@example.test", "denied identity must not survive inside unstructured upstream text")
			assert.NotContains(t, string(encoded), "secret=synthetic", "allowlisted envelope keys must not allow arbitrary upstream messages")
		})
	}
}

func TestRBACContractFullDataStillRedactsCredentials(t *testing.T) {
	access := &DataAccess{Role: common.RoleAdminUser, Policy: model.FullAdminDataPolicy()}
	for _, key := range []string{"password", "secret", "token", "access_token", "refresh_token", "session_key", "cookie", "cookies", "authorization", "base_url", "url", "credential", "api_key", "Authorization", "API_KEY"} {
		t.Run(key, func(t *testing.T) {
			input := map[string]any{"items": []any{map[string]any{"id": int64(9007199254740993), "tokens": 0, "email": "visible@example.test", "extension": map[string]any{key: "sensitive-sentinel", "label": "keep"}}}}
			projected, err := access.Project(input)
			require.NoError(t, err)
			encoded, err := json.Marshal(projected)
			require.NoError(t, err)
			assert.JSONEq(t, `{"items":[{"id":9007199254740993,"tokens":0,"email":"visible@example.test","extension":{"label":"keep"}}]}`, string(encoded))
		})
	}
}

func TestRBACContractCredentialRedactionRecursesThroughSpecialWrappers(t *testing.T) {
	access := &DataAccess{Role: common.RoleAdminUser, Policy: model.FullAdminDataPolicy()}
	for _, key := range []string{"status", "amounts", "costs_excluding_today"} {
		t.Run(key, func(t *testing.T) {
			input := map[string]any{"task_id": "task-1", key: map[string]any{"secret": "sensitive-sentinel", "USD": 12}}
			projected, err := access.Project(input)
			require.NoError(t, err)
			encoded, err := json.Marshal(projected)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "sensitive-sentinel", "special wrapper handling must not bypass credential recursion")
		})
	}
}

func TestRBACContractMetricObservationNeedsGroupAndTime(t *testing.T) {
	for _, tc := range []struct{ group, key string }{
		{"amount", "today_cost_observed_at"}, {"amount", "cost_7d_observed_at"},
		{"rates", "success_rate_observed_at"}, {"rpm", "rpm_observed_at"},
		{"concurrency", "concurrency_observed_at"}, {"vendor", "vendor_observed_at"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			for _, grants := range []map[string]bool{{}, {tc.group: true}, {"time": true}, {tc.group: true, "time": true}} {
				access := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
				access.Policy.Fields = grants
				projected, err := access.Project(map[string]any{tc.key: 123})
				require.NoError(t, err)
				if grants[tc.group] && grants["time"] {
					assert.Contains(t, projected, tc.key)
				} else {
					assert.NotContains(t, projected, tc.key)
				}
			}
		})
	}
}
