package mailbox

import (
	"context"
	"sort"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ArchiveInput struct {
	AccountType string       `json:"account_type"`
	Items       []AssignItem `json:"items"`
}

// Archiving keeps IDs and history intact. Lock accounts in the same order as
// assignment changes, and use CAS on SQLite where row locks are unavailable.
func (s *Service) ArchiveAccounts(ctx context.Context, actor Actor, input ArchiveInput, restore bool) error {
	var err error
	s, err = s.withAccountType(input.AccountType)
	if err != nil {
		return err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	if err := s.CheckActor(actor, authz.MailboxManage); err != nil {
		return err
	}
	if len(input.Items) == 0 || len(input.Items) > 1000 {
		return fail(400, "mailbox_invalid_request")
	}
	items := append([]AssignItem(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for i, item := range items {
		if item.ID <= 0 || item.Version <= 0 || (i > 0 && item.ID == items[i-1].ID) {
			return fail(400, "mailbox_invalid_request")
		}
	}
	action := "archive"
	if restore {
		action = "restore"
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		scoped := s.WithDB(tx)
		if err := scoped.workflowActor(actor, authz.MailboxManage); err != nil {
			return err
		}
		for _, item := range items {
			var account model.MailboxAccount
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_type = ?", s.pool()).First(&account, item.ID).Error; err != nil {
				return workflowNotFound(err)
			}
			if account.Version != item.Version {
				return fail(409, "mailbox_version_conflict")
			}
			if account.ActiveAssignmentID != 0 {
				return fail(409, "mailbox_archive_assigned")
			}
			if (account.ArchivedAt != 0) != restore {
				return fail(409, "mailbox_version_conflict")
			}
			at, by := s.Now().Unix(), actor.Admin.UserID
			if restore {
				at, by = 0, 0
			}
			result := tx.Model(&model.MailboxAccount{}).Where("id = ? AND version = ? AND active_assignment_id = 0", account.ID, item.Version).Updates(map[string]any{
				"archived_at": at, "archived_by": by, "version": gorm.Expr("version + 1"), "updated_at": s.Now().Unix(),
			})
			if err := workflowCAS(result); err != nil {
				return err
			}
			if err := scoped.Audit(actor, action, account.ID, 0, 200, ""); err != nil {
				return err
			}
		}
		return scoped.CheckActor(actor, authz.MailboxManage)
	})
	if err != nil {
		return err
	}
	return nil
}
