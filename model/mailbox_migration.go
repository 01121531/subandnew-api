package model

import "gorm.io/gorm"

// MigrateMailboxPools owns account schema migration. AutoMigrate interprets the
// old email index as a column UNIQUE on SQLite and attempts an unsafe rebuild.
// Add only the new columns, then replace indexes without reinserting any rows.
func MigrateMailboxPools(db *gorm.DB) error {
	if err := db.AutoMigrate(&MailboxAudit{}); err != nil {
		return err
	}
	migrate := func(tx *gorm.DB) error {
		migrator := tx.Migrator()
		if !migrator.HasTable(&MailboxAccount{}) {
			if err := migrator.CreateTable(&MailboxAccount{}); err != nil {
				return err
			}
		} else {
			for _, field := range []string{"AccountType", "CardCiphertext", "CardKeyVersion", "CardLast4", "ArchivedAt", "ArchivedBy"} {
				if !migrator.HasColumn(&MailboxAccount{}, field) {
					if err := migrator.AddColumn(&MailboxAccount{}, field); err != nil {
						return err
					}
				}
			}
		}
		if err := tx.Exec("UPDATE mailbox_accounts SET account_type = 'refund' WHERE account_type IS NULL OR account_type = ''").Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE mailbox_audits SET account_type = 'refund' WHERE account_type IS NULL OR account_type = ''").Error; err != nil {
			return err
		}
		// Use GORM's dialect-aware, table-scoped metadata and known v1.2.77
		// index name, never a catalog-wide search for an arbitrary email index.
		for _, index := range []string{"idx_mailbox_accounts_type_email", "idx_mailbox_accounts_active_assignment_id", "idx_mailbox_accounts_archived_at"} {
			if !migrator.HasIndex(&MailboxAccount{}, index) {
				if err := migrator.CreateIndex(&MailboxAccount{}, index); err != nil {
					return err
				}
			}
		}
		if migrator.HasIndex(&MailboxAccount{}, "idx_mailbox_accounts_email") {
			return migrator.DropIndex(&MailboxAccount{}, "idx_mailbox_accounts_email")
		}
		return nil
	}
	// MySQL DDL implicitly commits. Every step is independently resumable, and
	// the composite uniqueness guarantee is in place before the old one goes.
	if db.Dialector.Name() == "mysql" {
		return migrate(db)
	}
	return db.Transaction(migrate)
}
