package mailbox

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func operatorTestService(t *testing.T) (*Service, Actor) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.MailboxOperator{}, &model.MailboxSession{}, &model.MailboxLoginAttempt{}, &model.MailboxAudit{}))
	root := model.User{Username: "mailbox-test-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	access, err := authz.LoadDataAccess(db, root.Id)
	require.NoError(t, err)
	s := New(db)
	s.Now = func() time.Time { return time.Unix(1800000000, 0) }
	return s, Actor{Admin: access, IP: "192.0.2.1"}
}

func operatorTestCreate(t *testing.T, s *Service, admin Actor, username string) *OperatorView {
	t.Helper()
	item, err := s.SaveOperator(context.Background(), admin, 0, OperatorInput{Username: username, DisplayName: "Mailbox operator", Password: "password-123", Enabled: true})
	require.NoError(t, err)
	return item
}

func operatorTestError(t *testing.T, err error, status int, code string) {
	t.Helper()
	require.Error(t, err)
	actualStatus, actualCode := HTTPError(err)
	require.Equal(t, status, actualStatus)
	require.Equal(t, code, actualCode)
}

func TestMailboxOperatorsCreateListUpdate(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	item, err := s.SaveOperator(ctx, admin, 0, OperatorInput{Username: "  TeSt.User  ", DisplayName: "  First  ", Enabled: true})
	require.NoError(t, err)
	require.Equal(t, "test.user", item.Username)
	require.Equal(t, "First", item.DisplayName)
	require.Len(t, item.GeneratedPassword, 43)
	require.EqualValues(t, 1, item.Version)
	var stored model.MailboxOperator
	require.NoError(t, s.DB.First(&stored, item.ID).Error)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(item.GeneratedPassword)))
	require.NotEqual(t, item.GeneratedPassword, stored.PasswordHash)
	_, err = s.SaveOperator(ctx, admin, 0, OperatorInput{Username: "TEST.USER", DisplayName: "Duplicate", Password: "password-123", Enabled: true})
	operatorTestError(t, err, 409, "mailbox_operator_exists")
	operatorTestCreate(t, s, admin, "second.user")
	page, err := s.ListOperators(ctx, admin, ListQuery{PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.True(t, page.HasMore)
	require.Len(t, page.Items, 1)
	require.Empty(t, page.Items[0].GeneratedPassword)
	page, err = s.ListOperators(ctx, admin, ListQuery{Search: "test.user"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, secret := range []string{"password_hash", "auth_version", "generated_password", stored.PasswordHash, item.GeneratedPassword} {
		require.NotContains(t, string(encoded), secret)
	}
	input := OperatorInput{Username: item.Username, DisplayName: "Renamed", Enabled: true, Version: item.Version}
	updated, err := s.SaveOperator(ctx, admin, item.ID, input)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Version)
	require.Empty(t, updated.GeneratedPassword)
	_, err = s.SaveOperator(ctx, admin, item.ID, input)
	operatorTestError(t, err, 409, "mailbox_version_conflict")
	input.Version = updated.Version
	input.Username = "new.username"
	_, err = s.SaveOperator(ctx, admin, item.ID, input)
	operatorTestError(t, err, 400, "mailbox_username_immutable")
	input.Username = item.Username
	input.Password = "new-password"
	_, err = s.SaveOperator(ctx, admin, item.ID, input)
	operatorTestError(t, err, 400, "mailbox_password_reset_required")
	require.NoError(t, s.DB.First(&stored, item.ID).Error)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(item.GeneratedPassword)))
}

func TestMailboxOperatorsValidationAndPermissions(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	for _, password := range []string{"short", strings.Repeat("x", 73), strings.Repeat(" ", 8)} {
		_, err := s.SaveOperator(ctx, admin, 0, OperatorInput{Username: "valid.user", DisplayName: "Name", Password: password})
		operatorTestError(t, err, 400, "mailbox_invalid_password")
	}
	for _, input := range []OperatorInput{{Username: "x", DisplayName: "Name"}, {Username: "valid.user", DisplayName: " "}, {Username: "valid.user", DisplayName: strings.Repeat("x", 129)}} {
		_, err := s.SaveOperator(ctx, admin, 0, input)
		operatorTestError(t, err, 400, "mailbox_invalid_operator")
	}
	item := operatorTestCreate(t, s, admin, "portal.user")
	p, _, err := s.Login(ctx, item.Username, "password-123", "192.0.2.2")
	require.NoError(t, err)
	portal := PortalActor(p, "192.0.2.2")
	_, err = s.ListOperators(ctx, portal, ListQuery{})
	operatorTestError(t, err, 403, "mailbox_permission_denied")
	_, err = s.SaveOperator(ctx, portal, 0, OperatorInput{})
	operatorTestError(t, err, 403, "mailbox_permission_denied")
	_, err = s.ResetPassword(ctx, portal, item.ID, "")
	operatorTestError(t, err, 403, "mailbox_permission_denied")
	operatorTestError(t, s.RevokeSessions(ctx, portal, item.ID), 403, "mailbox_permission_denied")
	_, err = s.ListOperators(ctx, Actor{}, ListQuery{})
	operatorTestError(t, err, 401, "mailbox_session_expired")
	ordinary := model.User{Username: "ordinary-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, s.DB.Create(&ordinary).Error)
	access, err := authz.LoadDataAccess(s.DB, ordinary.Id)
	require.NoError(t, err)
	_, err = s.ListOperators(ctx, Actor{Admin: access}, ListQuery{})
	operatorTestError(t, err, 403, "mailbox_permission_denied")
	require.NoError(t, s.DB.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).UpdateColumn("authorization_version", admin.Admin.Version+1).Error)
	_, err = s.ListOperators(ctx, admin, ListQuery{})
	require.ErrorIs(t, err, authz.ErrAuthorizationChanged)
}

func TestMailboxOperatorsDisableResetRevoke(t *testing.T) {
	for _, operation := range []string{"disable", "reset", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			s, admin := operatorTestService(t)
			ctx := context.Background()
			item := operatorTestCreate(t, s, admin, "portal.user")
			p, raw, err := s.Login(ctx, item.Username, "password-123", "192.0.2.2")
			require.NoError(t, err)
			_, other, err := s.Login(ctx, item.Username, "password-123", "192.0.2.3")
			require.NoError(t, err)
			switch operation {
			case "disable":
				_, err = s.SaveOperator(ctx, admin, item.ID, OperatorInput{Username: item.Username, DisplayName: item.DisplayName, Enabled: false, Version: item.Version})
			case "reset":
				var password string
				password, err = s.ResetPassword(ctx, admin, item.ID, "")
				require.NoError(t, err)
				require.Len(t, password, 43)
				_, _, err = s.Login(ctx, item.Username, password, "192.0.2.4")
			case "revoke":
				err = s.RevokeSessions(ctx, admin, item.ID)
			}
			require.NoError(t, err)
			for _, token := range []string{raw, other} {
				_, err = s.Authenticate(token)
				operatorTestError(t, err, 401, "mailbox_session_expired")
			}
			operatorTestError(t, s.CheckActor(PortalActor(p, ""), authz.MailboxView), 401, "mailbox_session_expired")
			var stored model.MailboxOperator
			require.NoError(t, s.DB.First(&stored, item.ID).Error)
			require.EqualValues(t, 2, stored.AuthVersion)
			require.EqualValues(t, 2, stored.Version)
			var count int64
			require.NoError(t, s.DB.Model(&model.MailboxSession{}).Where("operator_id = ? AND auth_version = ?", item.ID, 1).Count(&count).Error)
			require.Zero(t, count)
			if operation == "disable" {
				_, _, err = s.Login(ctx, item.Username, "password-123", "192.0.2.5")
				operatorTestError(t, err, 401, "mailbox_invalid_credentials")
				_, err = s.SaveOperator(ctx, admin, item.ID, OperatorInput{Username: item.Username, DisplayName: item.DisplayName, Enabled: true, Version: stored.Version})
				require.NoError(t, err)
				_, err = s.Authenticate(raw)
				operatorTestError(t, err, 401, "mailbox_session_expired")
			}
		})
	}
}

func TestMailboxOperatorsAuditFailureRollsBack(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	item := operatorTestCreate(t, s, admin, "portal.user")
	_, raw, err := s.Login(ctx, item.Username, "password-123", "192.0.2.2")
	require.NoError(t, err)
	require.NoError(t, s.DB.Migrator().DropTable(&model.MailboxAudit{}))
	_, err = s.ResetPassword(ctx, admin, item.ID, "changed-password")
	require.Error(t, err)
	_, err = s.Authenticate(raw)
	require.NoError(t, err)
	var stored model.MailboxOperator
	require.NoError(t, s.DB.First(&stored, item.ID).Error)
	require.EqualValues(t, 1, stored.AuthVersion)
	require.EqualValues(t, 1, stored.Version)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("password-123")))
	_, err = s.SaveOperator(ctx, admin, 0, OperatorInput{Username: "rolled.back", DisplayName: "Name", Password: "password-123", Enabled: true})
	require.Error(t, err)
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxOperator{}).Where("username = ?", "rolled.back").Count(&count).Error)
	require.Zero(t, count)
}

func TestMailboxOperatorsAuditTargetsAndNoSecrets(t *testing.T) {
	for _, generated := range []bool{false, true} {
		name := "provided-passwords"
		if generated {
			name = "generated-passwords"
		}
		t.Run(name, func(t *testing.T) {
			s, admin := operatorTestService(t)
			ctx := context.Background()
			// Keep the affected operator distinct from both actor IDs and an incoming target hint.
			require.NoError(t, s.DB.Create(&model.MailboxOperator{ID: 91, Username: "other.operator", DisplayName: "Other", PasswordHash: operatorDummyHash}).Error)
			admin.TargetOperatorID = 777
			password, reset := "create-password-42", "reset-password-42"
			if generated {
				password, reset = "", ""
			}
			item, err := s.SaveOperator(ctx, admin, 0, OperatorInput{Username: "audit.operator", DisplayName: "Audit operator", Password: password, Enabled: true})
			require.NoError(t, err)
			require.NotEqual(t, int64(admin.Admin.UserID), item.ID)
			require.NotEqual(t, admin.TargetOperatorID, item.ID)
			if generated {
				password = item.GeneratedPassword
			}
			require.NotEmpty(t, password)
			var stored model.MailboxOperator
			require.NoError(t, s.DB.First(&stored, item.ID).Error)
			initialHash := stored.PasswordHash
			require.NotEmpty(t, initialHash)
			_, err = s.SaveOperator(ctx, admin, item.ID, OperatorInput{Username: item.Username, DisplayName: "Updated", Enabled: true, Version: item.Version})
			require.NoError(t, err)
			reset, err = s.ResetPassword(ctx, admin, item.ID, reset)
			require.NoError(t, err)
			require.NotEmpty(t, reset)
			require.NoError(t, s.RevokeSessions(ctx, admin, item.ID))
			require.NoError(t, s.DB.First(&stored, item.ID).Error)
			require.NotEmpty(t, stored.PasswordHash)
			require.NotEqual(t, initialHash, stored.PasswordHash)

			var audits []model.MailboxAudit
			require.NoError(t, s.DB.Order("id ASC").Find(&audits).Error)
			require.Len(t, audits, 4)
			for i, action := range []string{"operator_create", "operator_update", "operator_password_reset", "operator_sessions_revoke"} {
				require.Equal(t, action, audits[i].Action)
				require.Equal(t, admin.Admin.UserID, audits[i].AdminID)
				require.Zero(t, audits[i].OperatorID)
				require.Equal(t, item.ID, audits[i].TargetOperatorID)
				require.Equal(t, 200, audits[i].StatusCode)
			}
			require.EqualValues(t, 777, admin.TargetOperatorID)
			require.Zero(t, admin.OperatorID)

			// Inspect raw columns as well as JSON, so json:"-" cannot hide accidental persistence.
			var rows []map[string]any
			require.NoError(t, s.DB.Table("mailbox_audits").Find(&rows).Error)
			for _, value := range []any{audits, rows} {
				data, err := json.Marshal(value)
				require.NoError(t, err)
				require.Contains(t, string(data), `"target_operator_id"`)
				for _, secret := range []string{password, reset, initialHash, stored.PasswordHash, "password_hash", "generated_password"} {
					require.NotContains(t, string(data), secret)
				}
			}
		})
	}
}

func TestMailboxOperatorsConcurrentVersionAndStalePasswordWrite(t *testing.T) {
	s, admin := operatorTestService(t)
	item := operatorTestCreate(t, s, admin, "portal.user")
	var stale model.MailboxOperator
	require.NoError(t, s.DB.First(&stale, item.ID).Error)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"First edit", "Second edit"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, err := s.SaveOperator(context.Background(), admin, item.ID, OperatorInput{Username: item.Username, DisplayName: name, Enabled: true, Version: item.Version})
			results <- err
		}(name)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			operatorTestError(t, err, 409, "mailbox_version_conflict")
		}
	}
	require.Equal(t, 1, successes)
	err := s.DB.Transaction(func(tx *gorm.DB) error { return s.WithDB(tx).invalidateOperator(&stale, "stale-hash") })
	operatorTestError(t, err, 409, "mailbox_version_conflict")
}
