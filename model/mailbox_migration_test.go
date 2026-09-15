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
