package managedinstance

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedLegacyOrder(t *testing.T, db *gorm.DB, count int) []int64 {
	t.Helper()
	ids := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		instance := &model.ManagedInstance{Name: fmt.Sprintf("legacy-%d", i), Kind: model.ManagedInstanceKindGeneric, BaseURL: fmt.Sprintf("https://legacy-%d.example", i)}
		require.NoError(t, db.Create(instance).Error)
		ids = append([]int64{instance.Id}, ids...)
	}
	return ids
}

func orderIDs(view *OrderView) []int64 {
	ids := make([]int64, 0, len(view.Items))
	for _, item := range view.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestInstanceOrderLegacyAppendAndSave(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	ids := seedLegacyOrder(t, db, 3)
	initial, err := GetOrder()
	require.NoError(t, err)
	require.Equal(t, ids, orderIDs(initial))
	created, err := Create(CreateInput{Name: "appended", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://appended.example", TLSVerify: true, ActorID: 1})
	require.NoError(t, err)
	afterCreate, err := GetOrder()
	require.NoError(t, err)
	require.Equal(t, append(ids, created.Id), orderIDs(afterCreate))
	require.Greater(t, afterCreate.Version, initial.Version)
	_, err = SaveOrder(initial.Version, orderIDs(afterCreate), 1)
	require.ErrorIs(t, err, ErrInstanceOrderConflict)

	next := []int64{ids[1], created.Id, ids[2], ids[0]}
	saved, err := SaveOrder(afterCreate.Version, next, 1)
	require.NoError(t, err)
	require.Equal(t, next, orderIDs(saved))
	_, err = SaveOrder(afterCreate.Version, orderIDs(afterCreate), 2)
	require.ErrorIs(t, err, ErrInstanceOrderConflict)
	page, err := List(ListFilter{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, next[2], page.Items[0].Id)
	require.Equal(t, next[3], page.Items[1].Id)
	require.Equal(t, int64(4), page.Total)

	newest, err := Create(CreateInput{Name: "appended-again", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://appended-again.example", TLSVerify: true, ActorID: 1})
	require.NoError(t, err)
	current, err := GetOrder()
	require.NoError(t, err)
	require.Equal(t, append(next, newest.Id), orderIDs(current))
	require.NoError(t, Delete(ids[1], 1))
	_, err = SaveOrder(current.Version, orderIDs(current), 1)
	require.ErrorIs(t, err, ErrInstanceOrderConflict)
	current, err = GetOrder()
	require.NoError(t, err)
	require.Equal(t, []int64{created.Id, ids[2], ids[0], newest.Id}, orderIDs(current))
}

func TestInstanceOrderRejectsIncompleteOrInvalidIDsAtomically(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	ids := seedLegacyOrder(t, db, 3)
	initial, err := GetOrder()
	require.NoError(t, err)
	for _, invalid := range [][]int64{nil, {}, ids[:2], {ids[0], ids[0], ids[2]}, {ids[0], ids[1], -1}, {ids[0], ids[1], 999}, append(ids, 999)} {
		_, err := SaveOrder(initial.Version, invalid, 1)
		require.ErrorIs(t, err, ErrInvalidInstanceOrder)
		current, err := GetOrder()
		require.NoError(t, err)
		require.Equal(t, initial, current)
	}
	var count int64
	require.NoError(t, db.Model(&model.ManagedInstanceAudit{}).Where("action = ?", "order_update").Count(&count).Error)
	require.Zero(t, count)
}

func TestInstanceOrderEmptyAndNoOpVersions(t *testing.T) {
	newManagedInstanceTestDB(t)
	initial, err := GetOrder()
	require.NoError(t, err)
	require.NotNil(t, initial.Items)
	saved, err := SaveOrder(initial.Version, []int64{}, 1)
	require.NoError(t, err)
	require.Equal(t, initial.Version+1, saved.Version)
	_, err = SaveOrder(initial.Version, []int64{}, 2)
	require.ErrorIs(t, err, ErrInstanceOrderConflict)
}

func TestInstanceOrderWriteFailureRollsBackOrderAndVersion(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	ids := seedLegacyOrder(t, db, 3)
	initial, err := GetOrder()
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_order_audit BEFORE INSERT ON managed_instance_audits WHEN NEW.action = 'order_update' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error)
	_, err = SaveOrder(initial.Version, []int64{ids[2], ids[0], ids[1]}, 1)
	require.Error(t, err)
	current, err := GetOrder()
	require.NoError(t, err)
	require.Equal(t, initial, current)
}

func TestInstanceOrderFailedCreationDoesNotAdvanceVersion(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	seedLegacyOrder(t, db, 1)
	initial, err := GetOrder()
	require.NoError(t, err)
	_, err = Create(CreateInput{Name: "legacy-0", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://duplicate.example", TLSVerify: true, ActorID: 1})
	require.ErrorIs(t, err, ErrInstanceAlreadyExists)
	current, err := GetOrder()
	require.NoError(t, err)
	require.Equal(t, initial, current)
}

func TestInstanceOrderAllowedIDsIntersectFiltersAndPreserveOrder(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	ids := seedLegacyOrder(t, db, 3)
	allowed := []int64{ids[2], ids[0]}
	page, err := List(ListFilter{AllowedIDs: &allowed})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Equal(t, ids[0], page.Items[0].Id)
	require.Equal(t, ids[2], page.Items[1].Id)
	page, err = List(ListFilter{AllowedIDs: &allowed, IDs: []int64{ids[1], ids[2]}})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, ids[2], page.Items[0].Id)
	empty := []int64{}
	page, err = List(ListFilter{AllowedIDs: &empty})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Zero(t, page.Total)
	page, err = List(ListFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(3), page.Total)
}

func TestInstanceOrderConcurrentSavesHaveOneWinner(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "concurrent-order.db") + "?_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ManagedInstance{}, &model.ManagedInstanceOrderState{}, &model.ManagedInstanceAudit{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	ids := seedLegacyOrder(t, db, 3)
	initial, err := GetOrder()
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, next := range [][]int64{{ids[1], ids[0], ids[2]}, {ids[2], ids[1], ids[0]}} {
		go func(next []int64) {
			<-start
			_, err := SaveOrder(initial.Version, next, 1)
			results <- err
		}(next)
	}
	close(start)
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, ErrInstanceOrderConflict)
	} else {
		require.ErrorIs(t, first, ErrInstanceOrderConflict)
		require.NoError(t, second)
	}
	current, err := GetOrder()
	require.NoError(t, err)
	require.Equal(t, initial.Version+1, current.Version)
}

func TestInstanceOrderCreationGrantIsAtomic(t *testing.T) {
	for _, failure := range []string{"", "grant", "audit"} {
		t.Run("failure_"+failure, func(t *testing.T) {
			db := newManagedInstanceTestDB(t)
			ids := seedLegacyOrder(t, db, 2)
			user := model.User{Id: 2, Username: "creator", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			policy := model.AdminDataPolicy{UserID: user.Id, InstanceScope: "selected", InstanceIDs: []int64{ids[0]}, Fields: map[string]bool{"status": true}, Revision: 1}
			require.NoError(t, db.Create(&policy).Error)
			initial, err := GetOrder()
			require.NoError(t, err)
			if failure == "grant" {
				require.NoError(t, db.Exec("CREATE TRIGGER fail_grant BEFORE UPDATE ON admin_data_policies BEGIN SELECT RAISE(ABORT, 'grant failed'); END").Error)
			}
			if failure == "audit" {
				require.NoError(t, db.Exec("CREATE TRIGGER fail_create_audit BEFORE INSERT ON managed_instance_audits WHEN NEW.action = 'create' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error)
			}
			created, createErr := Create(CreateInput{Name: "granted", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://granted.example", TLSVerify: true, ActorID: user.Id,
				Credential: &CredentialInput{AuthType: "bearer_pat", Secret: "creation-test-secret"}})
			current, err := GetOrder()
			require.NoError(t, err)
			var stored model.AdminDataPolicy
			require.NoError(t, db.First(&stored, "user_id = ?", user.Id).Error)
			var storedUser model.User
			require.NoError(t, db.First(&storedUser, user.Id).Error)
			require.Equal(t, policy.Fields, stored.Fields)
			require.Equal(t, "selected", stored.InstanceScope)
			if failure == "" {
				require.NoError(t, createErr)
				require.Equal(t, append(ids, created.Id), orderIDs(current))
				require.ElementsMatch(t, []int64{ids[0], created.Id}, stored.InstanceIDs)
				require.Equal(t, policy.Revision+1, stored.Revision)
				require.Equal(t, user.AuthorizationVersion+1, storedUser.AuthorizationVersion)
			} else {
				require.Error(t, createErr)
				require.Nil(t, created)
				require.Equal(t, initial, current)
				require.Equal(t, policy, stored)
				require.Equal(t, user.AuthorizationVersion, storedUser.AuthorizationVersion)
				for _, table := range []any{&model.ManagedInstanceCredential{}, &model.ManagedInstanceAudit{}} {
					var count int64
					require.NoError(t, db.Model(table).Count(&count).Error)
					require.Zero(t, count)
				}
			}
		})
	}
}
