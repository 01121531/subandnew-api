package supplier

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestSupplierNamingValidation(t *testing.T) {
	for _, test := range []struct {
		name, prefix, suffix, base, expected string
		sk                                   bool
		code                                 string
	}{
		{name: "trim", prefix: "供货-", suffix: "-A", base: " 名称 ", expected: "供货-名称-A"},
		{name: "no-dedup", prefix: "P-", suffix: "-S", base: "P-name-S", expected: "P-P-name-S-S"},
		{name: "unicode-boundary", prefix: strings.Repeat("中", 63), base: "文", expected: strings.Repeat("中", 63) + "文"},
		{name: "overflow", prefix: strings.Repeat("中", 63), base: "文字", code: "supplier_account_name_too_long"},
		{name: "control", base: "name\n", code: "supplier_invalid_account_name"},
		{name: "empty", base: "  ", code: "supplier_invalid_account_name"},
		{name: "sk-prefix", prefix: "P-", base: "batch", sk: true, expected: "P-batch"},
		{name: "sk-suffix", suffix: "-S", base: "batch", sk: true, code: "supplier_sk_suffix_not_supported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &model.SupplierBinding{EffectiveNaming: &model.SupplierEffectiveNaming{SupplierNamingRule: model.SupplierNamingRule{Prefix: test.prefix, Suffix: test.suffix}, Version: "v1"}}
			in, err := applyUploadNaming(UploadInput{Name: test.base}, b, test.sk, time.Time{})
			if test.code != "" {
				requireCoreError(t, err, 400, test.code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, in.Name)
			require.Equal(t, "v1", in.NamingRevision)
			_, err = applyUploadNaming(UploadInput{Name: test.base, NamingRevision: "old"}, b, test.sk, time.Time{})
			requireCoreError(t, err, 409, "supplier_naming_changed")
		})
	}
	rule := &model.SupplierNamingRule{Prefix: "  中- ", Suffix: " -A  "}
	require.NoError(t, normalizeNaming(rule))
	require.Equal(t, "中-", rule.Prefix)
	require.Equal(t, "-A", rule.Suffix)
	for _, raw := range []string{`{"prefix":"x\n"}`, `{"suffix":"\u0000"}`, `{"unknown":true}`, `{"prefix":1}`} {
		_, err := parseNamingOverride(json.RawMessage(raw))
		requireCoreError(t, err, 400, "supplier_invalid_naming_rule")
	}
	require.Error(t, normalizeNaming(&model.SupplierNamingRule{Prefix: strings.Repeat("中", 64)}))
}

func TestSupplierNamingInheritanceAndLegacySave(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	second := f.binding(t, owner.ID, f.instance(t, 2).Id)
	owner, err := f.s.Save(owner.ID, SupplierInput{Name: owner.Name, Username: owner.Username, NamingRule: &model.SupplierNamingRule{Prefix: " V- ", Suffix: " -S "}})
	require.NoError(t, err)
	require.NotEmpty(t, owner.NamingChanges)
	require.Equal(t, "V-", owner.EffectiveNaming.Prefix)
	oldVersion := owner.EffectiveNaming.Version
	owner, err = f.s.Save(owner.ID, SupplierInput{Name: owner.Name, Username: owner.Username})
	require.NoError(t, err)
	require.Equal(t, oldVersion, owner.EffectiveNaming.Version)
	p, _ := f.login(t, owner.Username)
	b, err = f.s.authorize(p, b.ID, "upload")
	require.NoError(t, err)
	require.Equal(t, "V-", b.EffectiveNaming.Prefix)
	require.Equal(t, "supplier", b.EffectiveNaming.Source)
	verifies := f.r.verifies
	b, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{}`), NamingRevision: b.EffectiveNaming.Version})
	require.NoError(t, err)
	require.Empty(t, b.EffectiveNaming.Prefix)
	require.Empty(t, b.EffectiveNaming.Suffix)
	require.Equal(t, "binding", b.EffectiveNaming.Source)
	require.Equal(t, verifies, f.r.verifies)
	second, err = f.s.authorize(p, second.ID, "upload")
	require.NoError(t, err)
	require.Equal(t, "V-", second.EffectiveNaming.Prefix)
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`null`), NamingRevision: "stale"})
	requireCoreError(t, err, 409, "supplier_naming_changed")
	b, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`null`), NamingRevision: b.EffectiveNaming.Version})
	require.NoError(t, err)
	require.Nil(t, b.NamingOverride)
	require.Equal(t, "V-", b.EffectiveNaming.Prefix)
	_, err = f.s.Save(owner.ID, SupplierInput{Name: owner.Name, Username: owner.Username, NamingRevision: "old", NamingRule: &model.SupplierNamingRule{}})
	requireCoreError(t, err, 409, "supplier_naming_changed")
}

func TestSupplierNamingUploadsAndInvalidation(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	b, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{"prefix":"V-","suffix":"-S"}`)})
	require.NoError(t, err)
	p, _ := f.login(t, owner.Username)
	for _, method := range []string{"login", "setup_token"} {
		in := coreUpload(b.ID)
		in.Name = " name "
		in.OAuthFlow = method
		flow, err := f.s.StartUpload(context.Background(), p, in)
		require.NoError(t, err)
		require.Equal(t, "V-name-S", f.r.body["name"])
		require.NotContains(t, f.r.body, "naming_revision")
		_, err = f.s.Exchange(context.Background(), p, flow["flow_id"].(string), "code#test-state")
		require.NoError(t, err)
		require.Equal(t, "V-name-S", f.r.body["name"])
		require.Equal(t, method, f.r.body["oauth_flow"])
	}
	in := ImportAccountInput{UploadInput: coreUpload(b.ID), RefreshToken: "synthetic-rt"}
	in.Name = "RT"
	_, err = f.s.ImportAccounts(context.Background(), p, "rt", in)
	require.NoError(t, err)
	require.Equal(t, "V-RT-S", f.r.body["name"])
	in.RefreshToken = ""
	in.SessionKeys = []string{"synthetic-sk"}
	writes := f.r.writes
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	requireCoreError(t, err, 400, "supplier_sk_suffix_not_supported")
	require.Equal(t, writes, f.r.writes)
	flow, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
	require.NoError(t, err)
	b, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{"prefix":"new-"}`)})
	require.NoError(t, err)
	_, err = f.s.Exchange(context.Background(), p, flow["flow_id"].(string), "code#test-state")
	requireCoreError(t, err, 409, "supplier_oauth_flow_expired_or_used")
	in.NamingRevision = "old"
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	requireCoreError(t, err, 409, "supplier_naming_changed")
	in.NamingRevision = b.EffectiveNaming.Version
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	require.NoError(t, err)
	require.Equal(t, "new-RT", f.r.body["name"])
}

func TestSupplierNamingOptionsFreshOnCacheHit(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, owner.Username)
	first, err := f.s.Read(context.Background(), p, b.ID, "account-upload/options", url.Values{}, false)
	require.NoError(t, err)
	before := first["effective_naming"].(*model.SupplierEffectiveNaming).Version
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{"prefix":"new-"}`)})
	require.NoError(t, err)
	second, err := f.s.Read(context.Background(), p, b.ID, "account-upload/options", url.Values{}, false)
	require.NoError(t, err)
	require.Equal(t, 1, f.r.reads)
	rule := second["effective_naming"].(*model.SupplierEffectiveNaming)
	require.Equal(t, "new-", rule.Prefix)
	require.NotEqual(t, before, rule.Version)
}

type namingWriteRemote struct {
	*coreRemote
	after func()
}

func (r *namingWriteRemote) Write(ctx context.Context, method, resource string, body any) (map[string]any, error) {
	data, err := r.coreRemote.Write(ctx, method, resource, body)
	r.after()
	return data, err
}

func TestSupplierNamingChangeDuringAuthorization(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, owner.Username)
	remote := &namingWriteRemote{coreRemote: f.r, after: func() {
		_, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{"suffix":"-new"}`)})
		require.NoError(t, err)
	}}
	f.s.RemoteFactory = func(*model.ManagedInstance, string, string, string, string) (Remote, error) { return remote, nil }
	_, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
	requireCoreError(t, err, 409, "supplier_naming_changed")
	var count int64
	require.NoError(t, f.db.Model(&model.SupplierOAuthFlow{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestSupplierNamingNoOpPreservesFlowAndSupplierChangeRevokes(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, owner.Username)
	flow, err := f.s.StartUpload(context.Background(), p, coreUpload(b.ID))
	require.NoError(t, err)
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`null`)})
	require.NoError(t, err)
	require.NotZero(t, f.s.UploadBindingID(p, flow["flow_id"].(string)))
	_, err = f.s.Save(owner.ID, SupplierInput{Name: owner.Name, Username: owner.Username, NamingRule: &model.SupplierNamingRule{Suffix: "-new"}})
	require.NoError(t, err)
	var count int64
	require.NoError(t, f.db.Model(&model.SupplierOAuthFlow{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Model(&model.SupplierSession{}).Count(&count).Error)
	require.Zero(t, count)
}
