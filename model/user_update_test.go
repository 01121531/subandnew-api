package model

import (
	"errors"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupUserUpdateTestState(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
}

func TestUpdateUserSettingOnlyUpdatesSetting(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Username: "setting-user", Password: "password", Status: common.UserStatusEnabled, DisplayName: "before"}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "before", got.DisplayName)
	assert.Equal(t, "zh", got.GetSetting().Language)
}

func TestEnsureEmailAvailableRejectsExistingEmailCaseInsensitive(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.Create(&User{Username: "existing", Password: "old-password", Email: "Taken@Example.com", Status: common.UserStatusEnabled}).Error)
	require.ErrorIs(t, EnsureEmailAvailable(" taken@example.COM ", 0), ErrEmailAlreadyTaken)
	user, err := GetUniqueUserByEmail("TAKEN@example.com")
	require.NoError(t, err)
	assert.Equal(t, "existing", user.Username)
	require.NoError(t, EnsureEmailAvailable("taken@example.com", user.Id))
}

func TestInsertRejectsDuplicateEmail(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.Create(&User{Username: "existing", Password: "old-password", Email: "taken@example.com", Status: common.UserStatusEnabled}).Error)
	user := &User{Username: "oauth-user", Email: "TAKEN@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.ErrorIs(t, user.Insert(0), ErrEmailAlreadyTaken)
}

func TestInsertKeepsBlankPasswordForPasswordlessUser(t *testing.T) {
	setupUserUpdateTestState(t)
	user := &User{Username: "passwordless-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Empty(t, stored.Password)
}

func TestValidateAndFillRejectsPasswordlessUser(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.Create(&User{Username: "passwordless-user", Password: "", Status: common.UserStatusEnabled}).Error)
	err := (&User{Username: "passwordless-user", Password: "NewPassword123"}).ValidateAndFill()
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestResetUserPasswordByEmailRequiresSingleActiveMatch(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.Create(&User{Username: "duplicate-1", Password: "old-1", Email: "legacy@example.com", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{Username: "duplicate-2", Password: "old-2", Email: "LEGACY@example.com", Status: common.UserStatusEnabled}).Error)
	require.ErrorIs(t, ResetUserPasswordByEmail("legacy@example.com", "NewPassword123"), ErrEmailAmbiguous)
	require.NoError(t, DB.Create(&User{Username: "unique", Password: "old", Email: "unique@example.com", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, ResetUserPasswordByEmail("UNIQUE@example.com", "NewPassword123"))
	var unique User
	require.NoError(t, DB.Where("username = ?", "unique").First(&unique).Error)
	assert.True(t, common.ValidatePasswordAndHash("NewPassword123", unique.Password))
	err := ResetUserPasswordByEmail("missing@example.com", "NewPassword123")
	require.True(t, errors.Is(err, ErrEmailNotFound))
}
