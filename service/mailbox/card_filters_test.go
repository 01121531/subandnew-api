package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestCardFiltersAndFilteredExport(t *testing.T) {
	s, admin, _ := cvvTestService(t)
	ctx := context.Background()
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxSubmission{}, &model.MailboxAttachment{}, &model.MailboxRemarkRevision{}))
	for _, row := range []string{
		"first@example.test----synthetic----XXXX----4242424242424242----12/99----007",
		"second@example.test----synthetic----XXXX----5555555555554444----12/99",
	} {
		_, err := s.Import(ctx, admin, "text", []byte(row), AccountTypeOpening)
		require.NoError(t, err)
	}
	// No card is a valid legacy state and must survive exclusion.
	require.NoError(t, s.DB.Create(&model.MailboxAccount{AccountType: AccountTypeOpening, Email: "missing@example.test", Version: 1}).Error)
	tests := []struct {
		op     string
		values []string
		count  int64
	}{
		{"starts_with", []string{"42"}, 1}, {"not_starts_with", []string{"42"}, 2},
		{"ends_with", []string{"4444"}, 1}, {"not_ends_with", []string{"4444"}, 2},
		{"starts_with", []string{"42,55", "42"}, 2}, {"starts_with", []string{"00"}, 0},
	}
	for _, tt := range tests {
		q := ListQuery{AccountType: AccountTypeOpening, CardFilters: []CardFilter{{Operator: tt.op, Values: tt.values}}, PageSize: 1}
		page, err := s.ListAccounts(ctx, admin, q)
		require.NoError(t, err)
		require.Equal(t, tt.count, page.Total, tt.op)
	}
	filter := []CardFilter{{Operator: "starts_with", Values: []string{"42"}}}
	_, err := s.ListAccounts(ctx, admin, ListQuery{AccountType: AccountTypeRefund, CardFilters: filter})
	workflowTestStatus(t, err, 400)
	_, err = s.ListAccounts(ctx, admin, ListQuery{AccountType: AccountTypeOpening, CardFilters: []CardFilter{{Operator: "starts_with", Values: []string{"42x"}}}})
	workflowTestStatus(t, err, 400)
	payload, counts, err := s.ExportAllData(ctx, admin, AccountExportInput{Scope: "filtered", ListQuery: ListQuery{AccountType: AccountTypeOpening, CardFilters: filter}})
	require.NoError(t, err)
	require.Equal(t, 1, counts["邮箱资料"])
	book, err := excelize.OpenReader(bytes.NewReader(payload))
	require.NoError(t, err)
	defer book.Close()
	rows, err := book.GetRows("邮箱资料")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Contains(t, rows[1], "first@example.test")
	var indices []model.MailboxCardIndex
	require.NoError(t, s.DB.Find(&indices).Error)
	encoded, _ := json.Marshal(indices)
	require.NotContains(t, string(encoded), "4242424242424242")
	// Old accounts are indexed once; damaged ciphertext must not yield partial matches.
	require.NoError(t, s.DB.Exec("DELETE FROM mailbox_card_index_states").Error)
	page, err := s.ListAccounts(ctx, admin, ListQuery{AccountType: AccountTypeOpening, CardFilters: filter})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.NoError(t, s.DB.Exec("DELETE FROM mailbox_card_index_states").Error)
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("email = ?", "second@example.test").Update("card_ciphertext", "invalid").Error)
	_, err = s.ListAccounts(ctx, admin, ListQuery{AccountType: AccountTypeOpening, CardFilters: filter})
	workflowTestStatus(t, err, 503)
}

func TestCardReplacementClearsOnlyOldCVV(t *testing.T) {
	s, admin, owner := cvvTestService(t)
	a := cvvTestImport(t, s, admin, "007")
	assigned := cvvTestAssign(t, s, admin, owner, a.ID)
	workflowTestStatus(t, s.ClearCVV(context.Background(), owner, a.ID, TemporaryCVVInput{Version: assigned.Version}, AccountTypeOpening), 403)
	_, denied := s.ProvideTemporaryCVV(context.Background(), owner, a.ID, TemporaryCVVInput{Version: assigned.Version, CVV: "999"}, AccountTypeOpening)
	workflowTestStatus(t, denied, 403)
	_, denied = s.ListAccounts(context.Background(), owner, ListQuery{AccountType: AccountTypeOpening, CardFilters: []CardFilter{{Operator: "starts_with", Values: []string{"42"}}}})
	workflowTestStatus(t, denied, 403)
	data := []byte("cvv@example.test----synthetic----XXXX----5555555555554444----12/99")
	_, err := s.WithImportUpdateExisting(true).Import(context.Background(), admin, "text", data, AccountTypeOpening)
	require.NoError(t, err)
	_, err = s.Credentials(context.Background(), admin, a.ID, "cvv", AccountTypeOpening)
	workflowTestStatus(t, err, 404)
	_, err = s.WithImportUpdateExisting(true).Import(context.Background(), admin, "text", append(data, []byte("----0007")...), AccountTypeOpening)
	require.NoError(t, err)
	cvv, err := s.Credentials(context.Background(), owner, a.ID, "cvv", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, "0007", cvv.CVV)
	view, err := s.GetAccount(context.Background(), admin, a.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, assigned.AssignmentID, view.AssignmentID)
}
