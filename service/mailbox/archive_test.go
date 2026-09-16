package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMailboxArchiveRestoreAndImport(t *testing.T) {
	s, admin := newMailboxImportTestService(t)
	ctx := context.Background()
	a := mailboxImportTestAccount(t, s, admin, "archive@example.test", "  unchanged password  ", mailboxImportTestSecret)
	input := ArchiveInput{Items: []AssignItem{{a.ID, a.Version}}}
	require.NoError(t, s.ArchiveAccounts(ctx, admin, input, false))
	page, err := s.ListAccounts(ctx, admin, ListQuery{})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	page, err = s.ListAccounts(ctx, admin, ListQuery{Archived: true, Search: "ARCHIVE"})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.False(t, page.Items[0].CredentialsAvailable)
	require.EqualValues(t, admin.Admin.UserID, page.Items[0].ArchivedBy)
	var stored model.MailboxAccount
	require.NoError(t, s.DB.First(&stored, a.ID).Error)
	require.Equal(t, a.Ciphertext, stored.Ciphertext)
	require.Equal(t, a.KeyVersion, stored.KeyVersion)
	for _, kind := range []string{"password", "otp", "card", "cvv"} {
		value, err := s.Credentials(ctx, admin, a.ID, kind)
		require.Error(t, err)
		require.Nil(t, value)
	}
	data := []byte("archive@example.test----new password----" + mailboxImportTestSecret)
	preview, err := s.PreviewImport(ctx, admin, "text", data)
	require.NoError(t, err)
	require.False(t, preview.Valid)
	require.Equal(t, "mailbox_import_archived", preview.Issues[0].Code)
	_, err = s.Import(ctx, admin, "text", data)
	require.EqualError(t, err, "mailbox_import_archived")
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxAccount{}))
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxAccount{}))
	input.Items[0].Version = stored.Version
	require.NoError(t, s.ArchiveAccounts(ctx, admin, input, true))
	view, err := s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.Equal(t, "unassigned", view.Status)
	require.Zero(t, view.ArchivedAt)
	credential, err := s.Credentials(ctx, admin, a.ID, "password")
	require.NoError(t, err)
	require.Equal(t, "  unchanged password  ", credential.Password)
	var audits []model.MailboxAudit
	require.NoError(t, s.DB.Find(&audits).Error)
	encoded, err := json.Marshal(audits)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "unchanged password")
	require.NotContains(t, string(encoded), mailboxImportTestSecret)
}

func TestMailboxArchiveAtomicValidationAndPermissions(t *testing.T) {
	s, admin, operator, _ := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "free@example.test")
	b := workflowTestAccount(t, s, "assigned@example.test")
	assigned := workflowTestAssign(t, s, admin, operator, b.ID)
	input := ArchiveInput{Items: []AssignItem{{a.ID, a.Version}, {b.ID, assigned.Version}}}
	require.EqualError(t, s.ArchiveAccounts(ctx, admin, input, false), "mailbox_archive_assigned")
	var current model.MailboxAccount
	require.NoError(t, s.DB.First(&current, a.ID).Error)
	require.Zero(t, current.ArchivedAt)
	require.Equal(t, a.Version, current.Version)
	workflowTestStatus(t, s.ArchiveAccounts(ctx, operator, input, false), 403)
	_, err := s.ListAccounts(ctx, operator, ListQuery{Archived: true})
	workflowTestStatus(t, err, 403)
	require.NoError(t, s.DB.Create(&model.User{Id: 100, Username: "archive-restricted", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	access, err := authz.LoadDataAccess(s.DB, 100)
	require.NoError(t, err)
	workflowTestStatus(t, s.ArchiveAccounts(ctx, Actor{Admin: access}, input, false), 403)
	for _, bad := range []ArchiveInput{
		{}, {Items: make([]AssignItem, 1001)}, {Items: []AssignItem{{a.ID, a.Version}, {a.ID, a.Version}}},
		{Items: []AssignItem{{a.ID, 0}}}, {AccountType: "wrong", Items: input.Items},
		{AccountType: AccountTypeOpening, Items: []AssignItem{{a.ID, a.Version}}},
		{Items: []AssignItem{{a.ID, a.Version}, {b.ID, 999}}},
	} {
		require.Error(t, s.ArchiveAccounts(ctx, admin, bad, false))
	}
	require.NoError(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{b.ID, assigned.Version}}}))
	current = model.MailboxAccount{}
	require.NoError(t, s.DB.First(&current, b.ID).Error)
	input.Items[1].Version = current.Version
	require.NoError(t, s.ArchiveAccounts(ctx, admin, input, false))
	page, err := s.ListAccounts(ctx, admin, ListQuery{Archived: true, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.True(t, page.HasMore)
	workflowTestStatus(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{b.ID, current.Version + 1}}, OperatorID: operator.OperatorID}), 409)
	input.Items[0].Version++
	input.Items[1].Version++
	input.Items[1].Version++
	require.Error(t, s.ArchiveAccounts(ctx, admin, input, true))
	current = model.MailboxAccount{}
	require.NoError(t, s.DB.First(&current, a.ID).Error)
	require.NotZero(t, current.ArchivedAt)
}

func TestMailboxArchivePreservesHistoryAndScreenshots(t *testing.T) {
	s, admin, operator, other := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "history@example.test")
	workflowTestAssign(t, s, admin, operator, a.ID)
	submission := workflowTestSubmission(t, s, operator, a.ID)
	view, err := s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{a.ID, view.Version}}}))
	require.NoError(t, s.ArchiveAccounts(ctx, admin, ArchiveInput{Items: []AssignItem{{a.ID, view.Version + 1}}}, false))
	for _, actor := range []Actor{admin, operator} {
		page, err := s.ListSubmissions(ctx, actor, ListQuery{})
		require.NoError(t, err)
		require.EqualValues(t, 1, page.Total)
		require.Equal(t, submission.ID, page.Items[0].ID)
	}
	page, err := s.ListSubmissions(ctx, other, ListQuery{})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	// The original operator can read their submitted image even after recall/archive.
	require.NotEmpty(t, submission.Attachments)
	_, _, err = s.ReadAttachment(ctx, operator, submission.Attachments[0].ID)
	require.NoError(t, err)
	_, _, err = s.ReadAttachment(ctx, other, submission.Attachments[0].ID)
	require.Error(t, err)
}

func TestMailboxArchiveWithholdsInFlightCredentialsAndCVV(t *testing.T) {
	s, admin := newMailboxImportTestService(t)
	a := mailboxImportTestAccount(t, s, admin, "late@example.test", "sensitive", mailboxImportTestSecret)
	original := s.Cipher
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		require.NoError(t, s.ArchiveAccounts(context.Background(), admin, ArchiveInput{Items: []AssignItem{{a.ID, a.Version}}}, false))
		return original()
	}
	value, err := s.Credentials(context.Background(), admin, a.ID, "password")
	require.Error(t, err)
	require.Nil(t, value)
	s, admin, operator := cvvTestService(t)
	a = cvvTestImport(t, s, admin, "007")
	input := ArchiveInput{AccountType: AccountTypeOpening, Items: []AssignItem{{a.ID, a.Version}}}
	require.NoError(t, s.ArchiveAccounts(context.Background(), admin, input, false))
	require.Empty(t, s.cvv.items)
	_, err = s.ProvideTemporaryCVV(context.Background(), admin, a.ID, TemporaryCVVInput{Version: a.Version + 1, CVV: "008"}, AccountTypeOpening)
	require.Error(t, err)
	input.Items[0].Version++
	require.NoError(t, s.ArchiveAccounts(context.Background(), admin, input, true))
	cvvTestAssign(t, s, admin, operator, a.ID)
	value, err = s.Credentials(context.Background(), operator, a.ID, "cvv", AccountTypeOpening)
	require.Error(t, err)
	require.Nil(t, value)
}

func TestMailboxArchiveConcurrentAssignAndAuditRollback(t *testing.T) {
	s, admin, operator, _ := workflowTestService(t)
	for i := 0; i < 6; i++ {
		a := workflowTestAccount(t, s, fmt.Sprintf("race-%d@example.test", i))
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() {
			<-start
			results <- s.ArchiveAccounts(context.Background(), admin, ArchiveInput{Items: []AssignItem{{a.ID, 1}}}, false)
		}()
		go func() {
			<-start
			results <- s.Assign(context.Background(), admin, AssignInput{Items: []AssignItem{{a.ID, 1}}, OperatorID: operator.OperatorID})
		}()
		close(start)
		wins := 0
		for j := 0; j < 2; j++ {
			if <-results == nil {
				wins++
			}
		}
		require.Equal(t, 1, wins)
		var current model.MailboxAccount
		require.NoError(t, s.DB.First(&current, a.ID).Error)
		require.False(t, current.ArchivedAt != 0 && current.ActiveAssignmentID != 0)
	}
	a := workflowTestAccount(t, s, "audit@example.test")
	require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register("archive_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "mailbox_audits" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}))
	defer s.DB.Callback().Create().Remove("archive_audit_failure")
	require.Error(t, s.ArchiveAccounts(context.Background(), admin, ArchiveInput{Items: []AssignItem{{a.ID, 1}}}, false))
	var current model.MailboxAccount
	require.NoError(t, s.DB.First(&current, a.ID).Error)
	require.Zero(t, current.ArchivedAt)
	require.EqualValues(t, 1, current.Version)
}

func TestMailboxImportOptionsAvailability(t *testing.T) {
	s, admin, _ := cvvTestService(t)
	for _, test := range []struct{ mode, node, reason string }{{"", "master", ""}, {"disabled", "master", "not_enabled"}, {"single_node", "slave", "node_unsupported"}, {"single_node", "master", ""}, {"invalid", "master", "not_enabled"}} {
		t.Setenv("MAILBOX_TEMP_CVV_MODE", test.mode)
		t.Setenv("NODE_TYPE", test.node)
		options, err := s.ImportOptions(admin)
		require.NoError(t, err)
		require.Equal(t, test.reason, options["temporary_cvv_unavailable_reason"])
		require.Equal(t, test.reason == "", options["temporary_cvv_enabled"])
	}
}
