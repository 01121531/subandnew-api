package supplier

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func boolPtr(v bool) *bool { return &v }

func TestSupplierPolicyInheritanceAndInvalidation(t *testing.T) {
	f := newCoreFixture(t)
	owner, err := f.s.Save(0, SupplierInput{Name: "vendor", Username: "vendor", Password: corePassword})
	require.NoError(t, err)
	require.Empty(t, owner.PolicyOverrides)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	b2 := f.binding(t, owner.ID, f.instance(t, 2).Id)
	p, _ := f.login(t, "vendor")
	_, err = f.s.authorize(p, b.ID, "upload")
	requireCoreError(t, err, 403, "supplier_permission_denied")
	overrides := model.SupplierPolicy{"upload_accounts": boolPtr(true), "account.email": boolPtr(false)}
	before := f.r.verifies
	updated, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{PolicyOverrides: &overrides, PolicyRevision: b.EffectivePolicy.Version})
	require.NoError(t, err)
	require.Equal(t, before, f.r.verifies, "policy-only edits never authenticate upstream")
	require.Equal(t, b.Revision, updated.Revision)
	require.Equal(t, "binding", updated.EffectivePolicy.Sources["account.email"])
	require.Contains(t, updated.PolicyChanges, "account.email")
	_, err = f.s.authorize(p, b.ID, "accounts")
	requireCoreError(t, err, 401, "supplier_unauthenticated")
	p, _ = f.login(t, "vendor")
	_, err = f.s.authorize(p, b.ID, "upload")
	require.NoError(t, err)
	_, err = f.s.authorize(p, b2.ID, "upload")
	requireCoreError(t, err, 403, "")
	flow, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
	require.NoError(t, err)
	require.NotEmpty(t, flow["flow_id"])
	var count int64
	require.NoError(t, f.db.Model(&model.SupplierOAuthFlow{}).Where("supplier_id = ?", owner.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	defaults, err := f.s.DefaultPolicy()
	require.NoError(t, err)
	defaults.Policy["account.total_cost"] = boolPtr(false)
	next, changes, err := f.s.SaveDefaultPolicy(defaults.Policy, defaults.Revision)
	require.NoError(t, err)
	require.Contains(t, changes, "account.total_cost")
	require.Greater(t, next.Revision, defaults.Revision)
	_, err = f.s.authorize(p, b.ID, "accounts")
	requireCoreError(t, err, 401, "")
	require.NoError(t, f.db.Model(&model.SupplierOAuthFlow{}).Where("supplier_id = ?", owner.ID).Count(&count).Error)
	require.Zero(t, count)
	_, _, err = f.s.SaveDefaultPolicy(defaults.Policy, defaults.Revision)
	requireCoreError(t, err, 409, "")
	p, _ = f.login(t, "vendor")
	permitted, err := f.s.authorize(p, b2.ID, "accounts")
	require.NoError(t, err)
	require.False(t, permitted.EffectivePolicy.Values["account.total_cost"])
	require.Equal(t, "global", permitted.EffectivePolicy.Sources["account.total_cost"])
	overrides = model.SupplierPolicy{}
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{PolicyOverrides: &overrides})
	require.NoError(t, err)
	p, _ = f.login(t, "vendor")
	_, err = f.s.authorize(p, b.ID, "upload")
	requireCoreError(t, err, 403, "")
}

func TestSupplierPolicyMigrationPreservesLegacyAndEmptyInheritance(t *testing.T) {
	f := newCoreFixture(t)
	legacy := model.Supplier{Name: "legacy", Username: "legacy", Enabled: true, ViewAccounts: false, ViewUsage: true, ManageProxies: true, UploadAccounts: false}
	require.NoError(t, f.db.Create(&legacy).Error)
	inherited, err := f.s.Save(0, SupplierInput{Name: "new", Username: "new", Password: corePassword})
	require.NoError(t, err)
	require.NoError(t, model.MigrateSupplierPolicies(f.db))
	require.NoError(t, model.MigrateSupplierPolicies(f.db))
	require.NoError(t, f.db.First(&legacy, legacy.ID).Error)
	require.Len(t, legacy.PolicyOverrides, len(model.SupplierPolicyKeys))
	require.False(t, *legacy.PolicyOverrides["view_accounts"])
	require.True(t, *legacy.PolicyOverrides["manage_proxies"])
	require.True(t, *legacy.PolicyOverrides["account.email"])
	require.EqualValues(t, 1, legacy.PolicyVersion)
	require.NoError(t, f.db.First(inherited, inherited.ID).Error)
	require.Empty(t, inherited.PolicyOverrides)
	defaults, err := f.s.DefaultPolicy()
	require.NoError(t, err)
	defaults.Policy["usage.cost"] = boolPtr(false)
	updated, _, err := f.s.SaveDefaultPolicy(defaults.Policy, defaults.Revision)
	require.NoError(t, err)
	require.True(t, model.ResolveSupplierPolicy(updated, legacy, nil).Values["usage.cost"])
	require.False(t, model.ResolveSupplierPolicy(updated, *inherited, nil).Values["usage.cost"])
	require.NoError(t, model.MigrateSupplierPolicies(f.db))
	persisted, err := f.s.DefaultPolicy()
	require.NoError(t, err)
	require.Equal(t, updated.Revision, persisted.Revision)
}

func TestSupplierPolicyValidationLegacyInputAndHardDisable(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	bad := model.SupplierPolicy{"secret": boolPtr(true)}
	_, err := f.s.Save(owner.ID, SupplierInput{Name: owner.Name, Username: owner.Username, PolicyOverrides: &bad})
	requireCoreError(t, err, 400, "supplier_invalid_policy")
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{PolicyOverrides: &bad})
	requireCoreError(t, err, 400, "supplier_invalid_policy")
	_, _, err = f.s.SaveDefaultPolicy(model.SupplierPolicy{}, 1)
	requireCoreError(t, err, 400, "")
	overrides := model.SupplierPolicy{"account.email": boolPtr(false)}
	saved, err := f.s.Save(owner.ID, SupplierInput{Name: owner.Name, Username: owner.Username, PolicyOverrides: &overrides})
	require.NoError(t, err)
	saved, err = f.s.Save(owner.ID, SupplierInput{Name: owner.Name, Username: owner.Username, UploadAccounts: boolPtr(true)})
	require.NoError(t, err)
	require.False(t, saved.EffectivePolicy.Values["account.email"])
	require.True(t, saved.EffectivePolicy.Values["upload_accounts"])
	p, _ := f.login(t, "vendor")
	overrides = model.SupplierPolicy{"view_accounts": boolPtr(true)}
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{PolicyOverrides: &overrides, Enabled: boolPtr(false)})
	require.NoError(t, err)
	p, _ = f.login(t, "vendor")
	_, err = f.s.authorize(p, b.ID, "accounts")
	require.Error(t, err)
	other := f.supplier(t, "other", true)
	_, err = f.s.SaveBinding(context.Background(), other.ID, b.ID, BindingInput{PolicyOverrides: &overrides})
	requireCoreError(t, err, 404, "")
}

type policyRemote struct {
	*coreRemote
	data map[string]any
}

func (r *policyRemote) Read(ctx context.Context, resource string, q url.Values) (map[string]any, error) {
	_, err := r.coreRemote.Read(ctx, resource, q)
	return r.data, err
}

func TestSupplierPolicyReadRedactsCacheFallbackAndInFlight(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	remote := &policyRemote{coreRemote: f.r, data: map[string]any{"items": []any{map[string]any{"id": "id", "name": "visible", "email": "hidden@example.test", "total_cost": 123, "today_cost": nil, "status": "active"}}, "total": 40}}
	f.s.RemoteFactory = func(*model.ManagedInstance, string, string, string, string) (Remote, error) { return remote, nil }
	for _, field := range accountDiagnosticFields {
		remote.data["items"].([]any)[0].(map[string]any)[field] = "private diagnostic"
	}
	overrides := model.SupplierPolicy{"account.email": boolPtr(false), "account.total_cost": boolPtr(false), "summary.total_accounts": boolPtr(false), "account.status": boolPtr(false)}
	for field, limit := range map[string]string{"rpm": "max_rpm", "tpm": "max_tpm", "concurrent": "max_concurrent", "active_sessions": "max_sessions"} {
		overrides["account."+field] = boolPtr(false)
		row := remote.data["items"].([]any)[0].(map[string]any)
		row[field], row[limit] = 12, 100
	}
	_, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{PolicyOverrides: &overrides})
	require.NoError(t, err)
	p, _ := f.login(t, "vendor")
	for _, q := range []url.Values{{"search": {"hidden"}}, {"sort": {"total_cost"}}, {"sort": {"rpm"}}, {"status": {"active"}}, {"recovery_window": {"1h"}}} {
		_, err = f.s.Read(context.Background(), p, b.ID, "accounts", q, false)
		requireCoreError(t, err, 403, "supplier_field_forbidden")
	}
	read := func(force bool) map[string]any {
		result, err := f.s.Read(context.Background(), p, b.ID, "accounts", url.Values{"page_size": {"20"}}, force)
		require.NoError(t, err)
		return result
	}
	for i := 0; i < 3; i++ {
		if i == 2 {
			f.now = f.now.Add(31 * time.Second)
			f.r.readErr = &RemoteError{Status: 502, Code: "upstream_unavailable"}
		}
		result := read(false)
		row := result["items"].([]any)[0].(map[string]any)
		require.NotContains(t, row, "email")
		require.NotContains(t, row, "total_cost")
		require.NotContains(t, row, "status")
		for _, field := range []string{"rpm", "max_rpm", "tpm", "max_tpm", "concurrent", "max_concurrent", "active_sessions", "max_sessions"} {
			require.NotContains(t, row, field)
		}
		for _, field := range accountDiagnosticFields {
			require.NotContains(t, row, field)
		}
		require.Contains(t, row, "today_cost")
		require.Nil(t, row["today_cost"])
		require.NotContains(t, result, "total")
		require.Equal(t, true, result["has_more"])
		require.Equal(t, i == 2, result["stale"])
	}
	require.Equal(t, 2, f.r.reads)
	f.r.readErr = nil
	f.r.onRead = func() {
		overrides["view_accounts"] = boolPtr(false)
		_, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{PolicyOverrides: &overrides})
		require.NoError(t, err)
	}
	_, err = f.s.Read(context.Background(), p, b.ID, "accounts", nil, true)
	requireCoreError(t, err, 401, "")
}

func TestSupplierPolicyRedactsEveryDataKey(t *testing.T) {
	for _, key := range model.SupplierPolicyKeys[4:] {
		t.Run(key, func(t *testing.T) {
			defaults := model.SupplierPolicyDefault{Policy: model.InitialSupplierPolicy()}
			defaults.Policy[key] = boolPtr(false)
			policy := model.ResolveSupplierPolicy(defaults, model.Supplier{PolicyOverrides: model.SupplierPolicy{}}, nil)
			row := map[string]any{}
			for _, field := range model.SupplierPolicyKeys[4:] {
				for i, c := range field {
					if c == '.' {
						row[field[i+1:]] = 0
						break
					}
				}
			}
			raw, _ := json.Marshal(map[string]any{"items": []any{row}, "days": []any{row}, "accounts": []any{row}, "total": 0})
			var result map[string]any
			require.NoError(t, json.Unmarshal(raw, &result))
			resource := "accounts"
			if key[:6] == "usage." {
				resource = "usage"
			}
			if key[:8] == "summary." {
				resource = "account-summary"
				result = row
			}
			redactPolicy(result, resource, policy, nil)
			// Zero remains a real value for all other authorized keys.
			field := ""
			for i, c := range key {
				if c == '.' {
					field = key[i+1:]
					break
				}
			}
			switch resource {
			case "accounts":
				require.NotContains(t, result["items"].([]any)[0], field)
			case "usage":
				require.NotContains(t, result["days"].([]any)[0], field)
				require.NotContains(t, result["accounts"].([]any)[0], field)
			default:
				require.NotContains(t, result, field)
			}
		})
	}
}

func TestSupplierRuntimePermissionsAndOldDefaultClient(t *testing.T) {
	f := newCoreFixture(t)
	defaults, err := f.s.DefaultPolicy()
	require.NoError(t, err)
	defaults.Policy["account.active_sessions"] = boolPtr(false)
	defaults, _, err = f.s.SaveDefaultPolicy(defaults.Policy, defaults.Revision)
	require.NoError(t, err)
	legacy := model.SupplierPolicy{}
	for key, value := range defaults.Policy {
		if _, added := model.SupplierRuntimePolicyParents[key]; !added {
			legacy[key] = value
		}
	}
	updated, _, err := f.s.SaveDefaultPolicy(legacy, defaults.Revision)
	require.NoError(t, err)
	require.False(t, *updated.Policy["account.active_sessions"])
	require.True(t, *updated.Policy["account.rpm"])
	_, _, err = f.s.SaveDefaultPolicy(nil, updated.Revision)
	requireCoreError(t, err, 400, "supplier_invalid_policy")
	for field, limit := range map[string]string{"rpm": "max_rpm", "tpm": "max_tpm", "concurrent": "max_concurrent", "active_sessions": "max_sessions"} {
		policy := model.ResolveSupplierPolicy(updated, model.Supplier{PolicyOverrides: model.SupplierPolicy{"account." + field: boolPtr(false)}}, nil)
		row := map[string]any{field: 12, limit: 100, "name": "visible"}
		result := map[string]any{"items": []any{row}}
		redactPolicy(result, "accounts", policy, nil)
		require.NotContains(t, row, field)
		require.NotContains(t, row, limit)
		require.Equal(t, "visible", row["name"])
	}
}
