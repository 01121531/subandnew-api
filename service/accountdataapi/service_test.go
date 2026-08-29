package accountdataapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAPIServiceTest(t *testing.T) (*gorm.DB, model.ManagedInstance) {
	t.Helper()
	previous := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedInstanceSnapshot{},
		&model.ManagedAccountSnapshot{}, &model.SystemTask{}, &model.SystemTaskScopeLock{}, &model.ManagedAccountAPI{},
		&model.ManagedAccountAPIInstance{}, &model.ManagedAccountAPIKey{}, &model.ManagedAccountAPIAccessLog{},
		&model.ManagedAccountAPIPortalSession{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	instance := model.ManagedInstance{Name: "accounts", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://example.invalid"}
	require.NoError(t, db.Create(&instance).Error)
	now := time.Now().Unix()
	available := true
	payload, err := json.Marshal(managedinstance.InventoryPage{ResourceKind: "account", Items: []managedinstance.InventoryItem{
		{ID: 1, IDText: "acct-1", Name: "allowed", Email: "allowed@example.com", Enabled: &available, CreatedAt: now},
		{ID: 2, IDText: "acct-2", Name: "hidden", Email: "hidden@other.test", Enabled: &available, CreatedAt: now - 10},
	}, Total: 2})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: "inventory", Timezone: managedaccount.TimezoneShanghai, SchemaVersion: 2, ObservedAt: now, Payload: string(payload),
		LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded}).Error)
	return db, instance
}

func apiInput(instanceID int64) ConfigInput {
	return ConfigInput{Name: "partner-a", Dataset: managedaccount.DatasetInventory, PresetDays: 7, InstanceIDs: []int64{instanceID},
		IncludeTerms: []string{"example.com"}, MatchMode: managedinstance.AccountFilterMatchAll,
		Fields: []string{"name", "email", "available"}, SortBy: "name", SortOrder: "asc", PageSize: 50, RateLimitPerMinute: 60}
}

func TestCreateStoresOnlyHashAndQueriesFrozenFilter(t *testing.T) {
	db, instance := setupAPIServiceTest(t)
	created, err := Create(t.Context(), apiInput(instance.Id), 7)
	require.NoError(t, err)
	require.NotEmpty(t, created.Secret)
	require.True(t, strings.HasPrefix(created.Secret, "acct_live_"))
	var key model.ManagedAccountAPIKey
	require.NoError(t, db.First(&key).Error)
	require.NotContains(t, key.SecretHash, created.Secret)
	auth, err := Authenticate(created.Secret, "203.0.113.1")
	require.NoError(t, err)
	result, err := QueryExternal(t.Context(), auth, 1, 50, "", "", "")
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "allowed", result.Items[0].Name)
	projected := Project(result.Items[0], append([]string{"instance_id", "account_id"}, auth.View.Fields...))
	require.Equal(t, "acct-1", projected["account_id"])
	require.NotContains(t, projected, "note")
}

func TestKeysCanOverlapThenBeRevoked(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	created, err := Create(t.Context(), apiInput(instance.Id), 7)
	require.NoError(t, err)
	second, secret, err := CreateKey(created.API.ID, "replacement", 0, 7)
	require.NoError(t, err)
	require.NotEmpty(t, secret)
	_, _, err = CreateKey(created.API.ID, "third", 0, 7)
	require.ErrorIs(t, err, ErrTooManyKeys)
	require.NoError(t, RevokeKey(created.API.ID, second.ID))
	_, err = Authenticate(secret, "203.0.113.1")
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestCIDRAndDisabledAuthorizationAreEnforced(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	input.AllowedCIDRs = []string{"203.0.113.0/24"}
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	auth, err := Authenticate(created.Secret, "198.51.100.1")
	require.ErrorIs(t, err, ErrIPDenied)
	require.NotNil(t, auth)
	input.Status = model.ManagedAccountAPIDisabled
	_, err = Update(t.Context(), created.API.ID, input, 7)
	require.NoError(t, err)
	auth, err = Authenticate(created.Secret, "203.0.113.4")
	require.ErrorIs(t, err, ErrDisabled)
	require.NotNil(t, auth)
}

func TestInvalidConfigurationIsClassifiedAndHiddenSortIsRejected(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	input.AllowedCIDRs = []string{"not-a-network"}
	_, err := Create(t.Context(), input, 7)
	require.ErrorIs(t, err, ErrInvalid)

	input = apiInput(instance.Id + 9999)
	_, err = Create(t.Context(), input, 7)
	require.ErrorIs(t, err, ErrInvalid)

	input = apiInput(instance.Id)
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	auth, err := Authenticate(created.Secret, "203.0.113.1")
	require.NoError(t, err)
	_, err = QueryExternal(t.Context(), auth, 1, 20, "", "amount", "desc")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestKeyExpiryCannotExceedNinetyDays(t *testing.T) {
	_, instance := setupAPIServiceTest(t)
	created, err := Create(t.Context(), apiInput(instance.Id), 7)
	require.NoError(t, err)
	_, _, err = CreateKey(created.API.ID, "too-long", time.Now().Add(DefaultKeyLifetime+time.Hour).Unix(), 7)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestDisabledAuthorizationCanBeUpdatedWithoutSnapshot(t *testing.T) {
	db, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	require.NoError(t, db.Where("instance_id = ?", instance.Id).Delete(&model.ManagedAccountSnapshot{}).Error)
	input.Status = model.ManagedAccountAPIDisabled
	updated, err := Update(t.Context(), created.API.ID, input, 7)
	require.NoError(t, err)
	require.Equal(t, model.ManagedAccountAPIDisabled, updated.Status)
}

func TestLocalRateLimitIsPerKey(t *testing.T) {
	const keyID = int64(987654321)
	allowed, _ := AllowRequest(context.Background(), keyID, 2)
	require.True(t, allowed)
	allowed, _ = AllowRequest(context.Background(), keyID, 2)
	require.True(t, allowed)
	allowed, retryAfter := AllowRequest(context.Background(), keyID, 2)
	require.False(t, allowed)
	require.GreaterOrEqual(t, retryAfter, 1)
	require.LessOrEqual(t, retryAfter, 60)

	allowed, _ = AllowRequest(context.Background(), keyID+1, 2)
	require.True(t, allowed)
}
