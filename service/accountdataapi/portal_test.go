package accountdataapi

import (
	"bytes"
	"testing"

	"github.com/01121531/subandnew-api/model"
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
		{Field: "email", Operator: "contains", Values: []string{"example.com"}, ValueMode: "any"},
	}})
	require.ErrorIs(t, err, ErrInvalid)
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

func mustPortalCell(t *testing.T, workbook *excelize.File, cell string) string {
	t.Helper()
	value, err := workbook.GetCellValue("账号数据", cell)
	require.NoError(t, err)
	return value
}
