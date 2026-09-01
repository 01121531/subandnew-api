package accountdataapi

import (
	"bytes"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestPortalLoginQueryAndSessionRevocation(t *testing.T) {
	db, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	input.PortalEnabled = true
	input.PortalPassword = "portal-password-1"
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	require.True(t, created.API.PortalEnabled)
	require.True(t, created.API.PortalConfigured)
	require.Contains(t, created.API.PortalURL, "/account-data/")

	_, err = LoginPortal(created.API.PortalURL[len("/account-data/"):], "wrong-password", "203.0.113.5")
	require.ErrorIs(t, err, ErrPortalUnauthorized)
	login, err := LoginPortal(created.API.PortalURL[len("/account-data/"):], input.PortalPassword, "203.0.113.5")
	require.NoError(t, err)
	require.True(t, login.Session.Authenticated)
	require.NotEmpty(t, login.Token)
	require.NotEmpty(t, login.CSRFToken)

	auth, err := AuthenticatePortal(created.API.PortalURL[len("/account-data/"):], login.Token, login.CSRFToken, "203.0.113.5", true)
	require.NoError(t, err)
	csrfAgain, err := RefreshPortalCSRF(auth)
	require.NoError(t, err)
	require.Equal(t, login.CSRFToken, csrfAgain)
	result, err := QueryPortal(t.Context(), auth, PortalQueryInput{Page: 1, PageSize: 20, MatchMode: "all"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "acct-1", result.Items[0].AccountID)

	input.PortalPassword = "portal-password-2"
	_, err = Update(t.Context(), created.API.ID, input, 7)
	require.NoError(t, err)
	_, err = AuthenticatePortal(created.API.PortalURL[len("/account-data/"):], login.Token, login.CSRFToken, "203.0.113.5", true)
	require.ErrorIs(t, err, ErrPortalUnauthorized)
	var sessions int64
	require.NoError(t, db.Model(&model.ManagedAccountAPIPortalSession{}).Count(&sessions).Error)
	require.Zero(t, sessions)
}

func TestPortalFilterCannotUseHiddenFieldsOrExpandFixedScope(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	input.Fields = []string{"name", "available"}
	input.PortalEnabled = true
	input.PortalPassword = "portal-password"
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	slug := created.API.PortalURL[len("/account-data/"):]
	login, err := LoginPortal(slug, input.PortalPassword, "203.0.113.6")
	require.NoError(t, err)
	auth, err := AuthenticatePortal(slug, login.Token, login.CSRFToken, "203.0.113.6", true)
	require.NoError(t, err)

	result, err := QueryPortal(t.Context(), auth, PortalQueryInput{Page: 1, PageSize: 20, Search: "hidden", MatchMode: "all"})
	require.NoError(t, err)
	require.Zero(t, result.Total)
	_, err = QueryPortal(t.Context(), auth, PortalQueryInput{Page: 1, PageSize: 20, MatchMode: "all", Rules: []managedinstance.AccountFilterRule{
		{Field: "vendor_email", Operator: "contains", Values: []string{"example.com"}, ValueMode: "any"},
	}})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestPortalFilterFieldsIncludeOpenedMetrics(t *testing.T) {
	fields := PortalFilterFields([]string{"name", "vendor_name", "vendor_email", "requests", "amount", "cost_excluding_today", "rpm", "active_sessions", "utilization_5h", "created_at"})
	require.ElementsMatch(t, []string{"account_id", "name", "vendor_name", "vendor_email", "requests", "amount", "cost_excluding_today", "rpm", "active_sessions", "utilization_5h", "created_at"}, fields)
}

func TestPortalRequiresPasswordAndHonorsCIDR(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	input.PortalEnabled = true
	_, err := Create(t.Context(), input, 7)
	require.ErrorIs(t, err, ErrInvalid)

	input.PortalPassword = "portal-password"
	input.AllowedCIDRs = []string{"203.0.113.0/24"}
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	slug := created.API.PortalURL[len("/account-data/"):]
	_, err = LoginPortal(slug, input.PortalPassword, "198.51.100.2")
	require.ErrorIs(t, err, ErrPortalUnauthorized)
}

func TestPortalSelectedExportUsesCompositeIdentityAndTextIDs(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	input.IncludeTerms = nil
	input.Fields = []string{"name", "email", "created_at"}
	input.PortalEnabled = true
	input.PortalPassword = "portal-password"
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	slug := created.API.PortalURL[len("/account-data/"):]
	login, err := LoginPortal(slug, input.PortalPassword, "203.0.113.8")
	require.NoError(t, err)
	auth, err := AuthenticatePortal(slug, login.Token, login.CSRFToken, "203.0.113.8", true)
	require.NoError(t, err)

	export, err := ExportPortal(t.Context(), auth, PortalExportInput{Mode: "selected", Query: PortalQueryInput{MatchMode: "all"},
		Selections: []PortalSelection{{InstanceID: instance.Id, AccountID: "acct-2"}}})
	require.NoError(t, err)
	require.Equal(t, 1, export.Count)
	workbook, err := excelize.OpenReader(bytes.NewReader(export.Data))
	require.NoError(t, err)
	defer workbook.Close()
	require.Equal(t, "账号 ID", mustPortalCell(t, workbook, "B1"))
	require.Equal(t, "acct-2", mustPortalCell(t, workbook, "B2"))
	require.Equal(t, "hidden", mustPortalCell(t, workbook, "C2"))
}

func TestPortalFilteredExportAppliesExcludedAccounts(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	input.IncludeTerms = nil
	input.Fields = []string{"name"}
	input.PortalEnabled = true
	input.PortalPassword = "portal-password"
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	slug := created.API.PortalURL[len("/account-data/"):]
	login, err := LoginPortal(slug, input.PortalPassword, "203.0.113.9")
	require.NoError(t, err)
	auth, err := AuthenticatePortal(slug, login.Token, login.CSRFToken, "203.0.113.9", true)
	require.NoError(t, err)

	export, err := ExportPortal(t.Context(), auth, PortalExportInput{Mode: "filtered", Query: PortalQueryInput{MatchMode: "all"},
		Exclusions: []PortalSelection{{InstanceID: instance.Id, AccountID: "acct-2"}}})
	require.NoError(t, err)
	require.Equal(t, 1, export.Count)
	workbook, err := excelize.OpenReader(bytes.NewReader(export.Data))
	require.NoError(t, err)
	defer workbook.Close()
	require.Equal(t, "acct-1", mustPortalCell(t, workbook, "B2"))
}

func TestPortalWorkbookWritesScalarValuesAndShanghaiTimes(t *testing.T) {
	requests, tokens, amount, historicalCost, available := 1476.0, 66759000.0, 294.8689, 12840.12345678, true
	createdAt := time.Date(2026, time.August, 26, 5, 12, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)).Unix()
	lastActivityAt := createdAt + 31*60
	data, err := writePortalWorkbook([]string{"requests", "tokens", "amount", "cost_excluding_today", "available", "created_at", "last_activity_at", "vendor_name", "vendor_email"}, []managedaccount.Item{{
		InstanceID: 1, AccountID: "8faa3804-86ab-4f4c-a090-e5111a406c74", Requests: &requests, Tokens: &tokens,
		Amount: &amount, CostExcludingToday: &historicalCost, Available: &available, CreatedAt: createdAt, LastActivityAt: lastActivityAt,
		VendorName: "供应商 A", VendorEmail: "vendor@example.com",
	}})
	require.NoError(t, err)
	workbook, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer workbook.Close()

	require.Equal(t, "1476", mustPortalRawCell(t, workbook, "C2"))
	require.Equal(t, "66759000", mustPortalRawCell(t, workbook, "D2"))
	require.Equal(t, "294.8689", mustPortalRawCell(t, workbook, "E2"))
	require.Equal(t, "12840.12345678", mustPortalRawCell(t, workbook, "F2"))
	require.Equal(t, "TRUE", mustPortalCell(t, workbook, "G2"))
	require.Equal(t, "2026-08-26 05:12:00", mustPortalCell(t, workbook, "H2"))
	require.Equal(t, "2026-08-26 05:43:00", mustPortalCell(t, workbook, "I2"))
	require.Equal(t, "供应商 A", mustPortalCell(t, workbook, "J2"))
	require.Equal(t, "vendor@example.com", mustPortalCell(t, workbook, "K2"))
}

func TestPortalTimestampAcceptsSecondsMillisecondsAndRFC3339(t *testing.T) {
	want := int64(1787644320)
	for _, value := range []any{want, want * 1000, "1787644320000", "2026-08-25T15:52:00+08:00"} {
		actual, ok := portalTimestamp(value)
		require.True(t, ok)
		require.Equal(t, want, actual)
	}
}

func mustPortalCell(t *testing.T, workbook *excelize.File, cell string) string {
	t.Helper()
	value, err := workbook.GetCellValue("账号数据", cell)
	require.NoError(t, err)
	return value
}

func mustPortalRawCell(t *testing.T, workbook *excelize.File, cell string) string {
	t.Helper()
	value, err := workbook.GetCellValue("账号数据", cell, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	return value
}
