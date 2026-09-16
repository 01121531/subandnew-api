package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func TestMailboxPoolMigrationPreservesLegacyIDsAndHistory(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	require.NoError(t, db.Exec(`CREATE TABLE mailbox_accounts (id integer PRIMARY KEY AUTOINCREMENT, email text NOT NULL, ciphertext text NOT NULL, key_version text NOT NULL, version integer NOT NULL DEFAULT 1, active_assignment_id integer NOT NULL DEFAULT 0, created_by integer, created_at integer, updated_at integer)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_mailbox_accounts_email ON mailbox_accounts(email)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO mailbox_accounts (id,email,ciphertext,key_version,version,active_assignment_id) VALUES (42,'legacy@example.test','synthetic-legacy-ciphertext','test-v1',7,91)`).Error)
	require.NoError(t, db.AutoMigrate(&MailboxAssignment{}, &MailboxSubmission{}, &MailboxAttachment{}))
	require.NoError(t, db.Create(&MailboxAssignment{ID: 91, AccountID: 42, OperatorID: 3, Status: "approved", Version: 4}).Error)
	require.NoError(t, db.Create(&MailboxSubmission{ID: 101, AssignmentID: 91, OperatorID: 3, Status: "approved", Version: 2}).Error)
	require.NoError(t, db.Create(&MailboxAttachment{ID: "synthetic-attachment", AssignmentID: 91, SubmissionID: 101, StorageKey: "synthetic.png", ContentType: "image/png"}).Error)
	require.NoError(t, db.Exec(`CREATE TABLE mailbox_audits (id integer PRIMARY KEY AUTOINCREMENT, account_id integer, assignment_id integer, action text NOT NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO mailbox_audits(id,account_id,assignment_id,action) VALUES (11,42,91,'review')`).Error)
	for i := 0; i < 3; i++ {
		require.NoError(t, MigrateMailboxPools(db))
	}
	// Exercise ordinary startup too: the generic allowlist must not re-run
	// SQLite's unsafe account AutoMigrate after the explicit migration.
	for i := 0; i < 2; i++ {
		require.NoError(t, migrateDB())
	}
	var account MailboxAccount
	require.NoError(t, db.First(&account, 42).Error)
	require.Equal(t, MailboxAccountTypeRefund, account.AccountType)
	require.Equal(t, "synthetic-legacy-ciphertext", account.Ciphertext)
	require.Equal(t, "test-v1", account.KeyVersion)
	require.EqualValues(t, 7, account.Version)
	require.EqualValues(t, 91, account.ActiveAssignmentID)
	require.Empty(t, account.CardCiphertext)
	require.Zero(t, account.ArchivedAt)
	require.Zero(t, account.ArchivedBy)
	require.True(t, db.Migrator().HasIndex(&MailboxAccount{}, "idx_mailbox_accounts_archived_at"))
	var submission MailboxSubmission
	require.NoError(t, db.First(&submission, 101).Error)
	require.EqualValues(t, 91, submission.AssignmentID)
	require.Equal(t, "approved", submission.Status)
	var audit MailboxAudit
	require.NoError(t, db.First(&audit, 11).Error)
	require.Equal(t, MailboxAccountTypeRefund, audit.AccountType)
	require.EqualValues(t, 42, audit.AccountID)
	var attachment MailboxAttachment
	require.NoError(t, db.First(&attachment, "id = ?", "synthetic-attachment").Error)
	require.EqualValues(t, 101, attachment.SubmissionID)
	require.False(t, db.Migrator().HasIndex(&MailboxAccount{}, "idx_mailbox_accounts_email"))
	require.True(t, db.Migrator().HasIndex(&MailboxAccount{}, "idx_mailbox_accounts_type_email"))
	opening := MailboxAccount{AccountType: MailboxAccountTypeOpening, Email: account.Email, Ciphertext: "synthetic", KeyVersion: "test"}
	require.NoError(t, db.Create(&opening).Error)
	require.Greater(t, opening.ID, int64(42))
	require.Error(t, db.Create(&MailboxAccount{Email: account.Email, Ciphertext: "synthetic", KeyVersion: "test"}).Error)
	require.Error(t, db.Create(&MailboxAccount{AccountType: MailboxAccountTypeOpening, Email: account.Email, Ciphertext: "synthetic", KeyVersion: "test"}).Error)
	require.NoError(t, db.Model(&opening).Updates(map[string]any{"account_type": MailboxAccountTypeRefund, "version": 2}).Error)
	require.NoError(t, db.First(&opening, opening.ID).Error)
	require.Equal(t, MailboxAccountTypeOpening, opening.AccountType)
	opening.AccountType = MailboxAccountTypeRefund
	require.NoError(t, db.Save(&opening).Error)
	require.NoError(t, db.First(&opening, opening.ID).Error)
	require.Equal(t, MailboxAccountTypeOpening, opening.AccountType)
}

func TestMailboxArchiveMigrationPreservesArchivedRecords(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)
	require.NoError(t, MigrateMailboxPools(db))
	account := MailboxAccount{Email: "archived@example.test", Ciphertext: "synthetic encrypted data", KeyVersion: "test", Version: 4, ArchivedAt: 1780000000, ArchivedBy: 7}
	require.NoError(t, db.Create(&account).Error)
	for i := 0; i < 3; i++ {
		require.NoError(t, MigrateMailboxPools(db))
	}
	var current MailboxAccount
	require.NoError(t, db.First(&current, account.ID).Error)
	require.Equal(t, account, current)
	require.Error(t, db.Create(&MailboxAccount{Email: account.Email, Ciphertext: "other", KeyVersion: "test"}).Error)
}

func TestMailboxOperatorWorkIndexesMigrateIdempotently(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	models := []any{&MailboxAssignment{}, &MailboxSubmission{}, &MailboxIssue{}}
	indexes := []struct {
		table   string
		name    string
		columns []string
	}{
		{"mailbox_assignments", "idx_mailbox_assignments_operator_account_id", []string{"operator_id", "account_id", "id"}},
		{"mailbox_assignments", "idx_mailbox_assignments_operator_created_account", []string{"operator_id", "created_at", "account_id"}},
		{"mailbox_submissions", "idx_mailbox_submissions_operator_created_assignment", []string{"operator_id", "created_at", "assignment_id"}},
		{"mailbox_submissions", "idx_mailbox_submissions_assignment_operator_created", []string{"assignment_id", "operator_id", "created_at"}},
		{"mailbox_issues", "idx_mailbox_issues_operator_created_account", []string{"operator_id", "created_at", "account_id"}},
		{"mailbox_issues", "idx_mailbox_issues_operator_account_created", []string{"operator_id", "account_id", "created_at"}},
		{"mailbox_issues", "idx_mailbox_issues_assignment_operator_status", []string{"assignment_id", "operator_id", "status"}},
	}
	assertIndexes := func() {
		t.Helper()
		for _, index := range indexes {
			require.True(t, db.Migrator().HasIndex(index.table, index.name), index.name)
			var columns []string
			require.NoError(t, db.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", index.name).Scan(&columns).Error)
			require.Equal(t, index.columns, columns, index.name)
			var nonUnique int64
			require.NoError(t, db.Raw(`SELECT COUNT(*) FROM pragma_index_list(?) WHERE name = ? AND "unique" = 0`, index.table, index.name).Scan(&nonUnique).Error)
			require.EqualValues(t, 1, nonUnique, index.name)
		}
	}
	require.NoError(t, db.AutoMigrate(models...))
	assertIndexes()
	// Repeated events and reassignment generations must remain legal.
	assignments := []MailboxAssignment{
		{ID: 91, AccountID: 42, OperatorID: 3, Status: "rejected", Version: 4, RevokedAt: 200, CreatedAt: 100},
		{ID: 92, AccountID: 42, OperatorID: 3, Status: "submitted", Version: 2, CreatedAt: 100},
	}
	submissions := []MailboxSubmission{
		{ID: 101, AssignmentID: 91, OperatorID: 3, Status: "rejected", Version: 2, ReviewReason: "Synthetic reason", CreatedAt: 150},
		{ID: 102, AssignmentID: 91, OperatorID: 3, Status: "approved", Version: 2, CreatedAt: 150},
	}
	issues := []MailboxIssue{
		{ID: 111, AccountID: 42, AssignmentID: 91, OperatorID: 3, Kind: "other", Description: "First", Status: "resolved", Version: 2, CreatedAt: 120},
		{ID: 112, AccountID: 42, AssignmentID: 91, OperatorID: 3, Kind: "other", Description: "Second", Status: "resolved", Version: 2, CreatedAt: 120},
	}
	require.NoError(t, db.Create(&assignments).Error)
	require.NoError(t, db.Create(&submissions).Error)
	require.NoError(t, db.Create(&issues).Error)
	// Simulate the pre-statistics schema while preserving its history rows.
	for _, index := range indexes {
		require.NoError(t, db.Migrator().DropIndex(index.table, index.name))
		require.False(t, db.Migrator().HasIndex(index.table, index.name))
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, db.AutoMigrate(models...))
		assertIndexes()
		var currentAssignments []MailboxAssignment
		var currentSubmissions []MailboxSubmission
		var currentIssues []MailboxIssue
		require.NoError(t, db.Order("id").Find(&currentAssignments).Error)
		require.NoError(t, db.Order("id").Find(&currentSubmissions).Error)
		require.NoError(t, db.Order("id").Find(&currentIssues).Error)
		require.Equal(t, assignments, currentAssignments)
		require.Equal(t, submissions, currentSubmissions)
		require.Equal(t, issues, currentIssues)
		for table, names := range map[string][]string{
			"mailbox_assignments": {"idx_mailbox_assignments_account_id", "idx_mailbox_assignments_operator_id", "idx_mailbox_assignments_status"},
			"mailbox_submissions": {"idx_mailbox_submissions_assignment_id", "idx_mailbox_submissions_operator_id", "idx_mailbox_submissions_status", "idx_mailbox_submissions_created_at"},
			"mailbox_issues":      {"idx_mailbox_issues_account_id", "idx_mailbox_issues_assignment_id", "idx_mailbox_issues_operator_id", "idx_mailbox_issues_status", "idx_mailbox_issues_created_at", "idx_mailbox_issues_open_assignment_id"},
		} {
			for _, name := range names {
				require.True(t, db.Migrator().HasIndex(table, name), name)
			}
		}
	}
}
