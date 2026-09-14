package supplier

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestPortalSettingsMigrationValidationAndPolicyCompatibility(t *testing.T) {
	f := newCoreFixture(t)
	settings, err := f.s.PortalSettings()
	require.NoError(t, err)
	require.Equal(t, "工作台", settings.Title)
	for _, method := range uploadMethods {
		require.True(t, settings.UploadMethods[method])
	}
	settings.Title = "  测试工作台  "
	settings.UploadMethods["rt"] = false
	saved, changes, err := f.s.SavePortalSettings(settings)
	require.NoError(t, err)
	require.Contains(t, changes, "portal_settings")
	require.Equal(t, "测试工作台", saved.Title)
	require.Equal(t, int64(2), saved.Revision)
	_, _, err = f.s.SavePortalSettings(settings)
	requireCoreError(t, err, 409, "supplier_portal_settings_changed")
	for i := 0; i < 2; i++ {
		require.NoError(t, model.MigrateSupplierPolicies(f.db))
	}
	policy, err := f.s.DefaultPolicy()
	require.NoError(t, err)
	policy.Policy["view_usage"] = boolPtr(false)
	_, _, err = f.s.SaveDefaultPolicy(policy.Policy, policy.Revision)
	require.NoError(t, err)
	after, err := f.s.PortalSettings()
	require.NoError(t, err)
	require.Equal(t, saved, after)
	for _, title := range []string{"", " \t", "x\n", strings.Repeat("中", 65)} {
		bad := saved
		bad.Title = title
		_, _, err := f.s.SavePortalSettings(bad)
		requireCoreError(t, err, 400, "supplier_invalid_portal_text")
	}
	for _, value := range []string{"<b>plain</b>", strings.Repeat("中", 64)} {
		_, err := portalText(value, false)
		require.NoError(t, err)
	}
	saved.UploadMethods["other"] = true
	_, _, err = f.s.SavePortalSettings(saved)
	requireCoreError(t, err, 400, "supplier_invalid_upload_methods")
}

func TestPortalSettingsEnforcedForAllUploadRoutesAndFrozenFlows(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, owner.Username)
	ctx := context.Background()
	settings, err := f.s.PortalSettings()
	require.NoError(t, err)
	for _, method := range uploadMethods {
		settings.UploadMethods[method] = false
		settings, _, err = f.s.SavePortalSettings(settings)
		require.NoError(t, err)
		before := f.r.writes
		in := coreUpload(b.ID)
		if method == "rt" || method == "sk" {
			input := ImportAccountInput{UploadInput: in}
			if method == "rt" {
				input.RefreshToken = "synthetic-rt"
			} else {
				input.SessionKeys = []string{"synthetic-sk"}
			}
			_, err = f.s.ImportAccounts(ctx, p, method, input)
		} else {
			if method == "setup_token" {
				in.OAuthFlow = method
			}
			_, err = f.s.StartUpload(ctx, p, in)
		}
		requireCoreError(t, err, 403, "supplier_upload_method_disabled")
		require.Equal(t, before, f.r.writes)
	}
	bindings, err := f.s.PortalBindings(owner.ID)
	require.NoError(t, err)
	require.Empty(t, bindings[0].AllowedMethods)
	options, err := f.s.Read(ctx, p, b.ID, "account-upload/options", url.Values{}, false)
	require.NoError(t, err)
	require.Empty(t, options["allowed_upload_methods"])
	for _, method := range uploadMethods {
		settings.UploadMethods[method] = true
	}
	settings, _, err = f.s.SavePortalSettings(settings)
	require.NoError(t, err)
	for _, method := range []string{"login", "setup_token"} {
		in := coreUpload(b.ID)
		in.OAuthFlow = method
		flow, err := f.s.StartUpload(ctx, p, in)
		require.NoError(t, err)
		settings.UploadMethods[method] = false
		settings, _, err = f.s.SavePortalSettings(settings)
		require.NoError(t, err)
		settings.UploadMethods[method] = true
		settings, _, err = f.s.SavePortalSettings(settings)
		require.NoError(t, err)
		before := f.r.writes
		_, err = f.s.Exchange(ctx, p, flow["flow_id"].(string), "code#test-state")
		requireCoreError(t, err, 409, "supplier_oauth_flow_expired_or_used")
		require.Equal(t, before, f.r.writes)
	}
	in := coreUpload(b.ID)
	in.PortalRevision = settings.Revision - 1
	_, err = f.s.StartUpload(ctx, p, in)
	requireCoreError(t, err, 409, "supplier_portal_settings_changed")
}

func TestPortalAliasPrivacyAndNoReloginOrFlowInvalidation(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "private-vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	p, raw := f.login(t, owner.Username)
	flow, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
	require.NoError(t, err)
	verifies := f.r.verifies
	// Legacy inherited bindings may have NULL rather than an empty override map.
	require.NoError(t, f.db.Model(&model.SupplierBinding{}).Where("id = ?", b.ID).UpdateColumn("policy_overrides", nil).Error)
	name := "华东线路"
	updated, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{DisplayName: &name, Enabled: &b.Enabled, PolicyOverrides: &b.PolicyOverrides})
	require.NoError(t, err)
	require.Equal(t, name, *updated.DisplayName)
	require.Equal(t, b.Revision, updated.Revision)
	require.Equal(t, verifies, f.r.verifies)
	_, err = f.s.Authenticate(raw)
	require.NoError(t, err)
	settings, err := f.s.PortalSettings()
	require.NoError(t, err)
	settings.Title = "Public title"
	_, _, err = f.s.SavePortalSettings(settings)
	require.NoError(t, err)
	_, err = f.s.Exchange(context.Background(), p, flow["flow_id"].(string), "code#test-state")
	require.NoError(t, err)
	bindings, err := f.s.PortalBindings(owner.ID)
	require.NoError(t, err)
	require.Equal(t, name, bindings[0].DisplayName)
	payload, err := json.Marshal(bindings)
	require.NoError(t, err)
	for _, secret := range []string{"instance_name", "instance_id", "supplier_id", "remote_username", "gateway-1", "private-vendor", "verified-vendor", "base_url"} {
		require.NotContains(t, string(payload), secret)
	}
	name = ""
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{DisplayName: &name})
	require.NoError(t, err)
	bindings, err = f.s.PortalBindings(owner.ID)
	require.NoError(t, err)
	require.Equal(t, "线路 1", bindings[0].DisplayName)
	_, err = f.s.SaveBinding(context.Background(), owner.ID+1, b.ID, BindingInput{DisplayName: &name})
	require.Error(t, err)
}

func TestPortalMethodChangeDuringAuthorizationCannotCreateNewFlow(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, owner.Username)
	f.r.onWrite = func() {
		settings, err := f.s.PortalSettings()
		require.NoError(t, err)
		settings.UploadMethods["login"] = false
		_, _, err = f.s.SavePortalSettings(settings)
		require.NoError(t, err)
	}
	_, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
	requireCoreError(t, err, 403, "supplier_upload_method_disabled")
	var count int64
	require.NoError(t, f.db.Model(&model.SupplierOAuthFlow{}).Count(&count).Error)
	require.Zero(t, count)
}
