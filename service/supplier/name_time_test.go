package supplier

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestSupplierNameTimeFormatsAndBoundaries(t *testing.T) {
	at := time.Date(2026, 9, 11, 9, 12, 0, 0, time.UTC)
	b := &model.SupplierBinding{EffectiveNaming: &model.SupplierEffectiveNaming{SupplierNamingRule: model.SupplierNamingRule{Prefix: "V-", Suffix: "-S"}, Version: "v1"}}
	for _, tc := range []struct{ mode, want string }{{"", "V-账号-S"}, {"none", "V-账号-S"}, {"date", "V-账号-0911-S"}, {"date_time", "V-账号-0911-1712-S"}} {
		in := UploadInput{Name: " 账号 ", NameTimeMode: tc.mode, ProxyMode: "direct"}
		require.True(t, in.valid())
		actual, err := applyUploadNaming(in, b, false, at)
		require.NoError(t, err)
		require.Equal(t, tc.want, actual.Name)
		require.NotContains(t, actual.payload(), "name_time_mode")
	}
	actual, err := applyUploadNaming(UploadInput{Name: "账号", NameTimeMode: "date_time"}, b, false, time.Date(2026, 12, 31, 16, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "V-账号-0101-0000-S", actual.Name)
	for _, mode := range []string{"today", "DATE", "20260911"} {
		in := UploadInput{Name: "name", ProxyMode: "direct", NameTimeMode: mode}
		require.False(t, in.valid())
		_, err := applyUploadNaming(in, b, false, at)
		requireCoreError(t, err, 400, "supplier_invalid_upload_parameters")
	}
	for _, mode := range []string{"date", "date_time"} {
		suffixLen := 5
		if mode == "date_time" {
			suffixLen = 10
		}
		in := UploadInput{Name: strings.Repeat("中", 64-4-suffixLen), NameTimeMode: mode}
		_, err := applyUploadNaming(in, b, false, at)
		require.NoError(t, err)
		in.Name += "文"
		_, err = applyUploadNaming(in, b, false, at)
		requireCoreError(t, err, 400, "supplier_account_name_too_long")
	}
}

func TestSupplierNameTimeFrozenAcrossMidnight(t *testing.T) {
	for _, method := range []string{"login", "setup_token"} {
		t.Run(method, func(t *testing.T) {
			f := newCoreFixture(t)
			owner := f.supplier(t, "vendor", true)
			b := f.binding(t, owner.ID, f.instance(t, 1).Id)
			_, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{"prefix":"V-","suffix":"-S"}`)})
			require.NoError(t, err)
			f.now = time.Date(2026, 9, 11, 15, 59, 0, 0, time.UTC)
			p, _ := f.login(t, owner.Username)
			in := coreUpload(b.ID)
			in.Name = "账号"
			in.OAuthFlow = method
			in.NameTimeMode = "date_time"
			flow, err := f.s.StartUpload(context.Background(), p, in)
			require.NoError(t, err)
			require.Equal(t, "V-账号-0911-2359-S", flow["resolved_name"])
			f.now = f.now.Add(2 * time.Minute)
			result, err := f.s.Exchange(context.Background(), p, flow["flow_id"].(string), "code#test-state")
			require.NoError(t, err)
			require.Equal(t, flow["resolved_name"], result["resolved_name"])
			require.Equal(t, flow["resolved_name"], f.r.body["name"])
			require.NotContains(t, f.r.body, "name_time_mode")
			_, err = f.s.Exchange(context.Background(), p, flow["flow_id"].(string), "code#test-state")
			require.Error(t, err)
		})
	}
}

func TestSupplierNameTimeDirectImports(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "vendor", true)
	b := f.binding(t, owner.ID, f.instance(t, 1).Id)
	_, err := f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{"prefix":"V-"}`)})
	require.NoError(t, err)
	f.now = time.Date(2026, 9, 11, 9, 12, 0, 0, time.UTC)
	p, _ := f.login(t, owner.Username)
	in := ImportAccountInput{UploadInput: coreUpload(b.ID), RefreshToken: "synthetic-rt"}
	in.Name = "账号"
	in.NameTimeMode = "date"
	result, err := f.s.ImportAccounts(context.Background(), p, "rt", in)
	require.NoError(t, err)
	require.Equal(t, "V-账号-0911", result["resolved_name"])
	in.RefreshToken = ""
	in.SessionKeys = []string{"synthetic-a", "synthetic-b"}
	in.NameTimeMode = "date_time"
	result, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	require.NoError(t, err)
	require.Equal(t, "V-账号-0911-1712", result["resolved_name_prefix"])
	require.Equal(t, result["resolved_name_prefix"], f.r.body["name"])
	require.NotContains(t, result, "resolved_name")
	require.NotContains(t, f.r.body, "name_time_mode")
	_, err = f.s.SaveBinding(context.Background(), owner.ID, b.ID, BindingInput{NamingOverride: json.RawMessage(`{"suffix":"-S"}`)})
	require.NoError(t, err)
	writes := f.r.writes
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	requireCoreError(t, err, 400, "supplier_sk_suffix_not_supported")
	require.Equal(t, writes, f.r.writes)
}
