package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHardDeleteUserPurgesControlPlaneAuthenticationData(t *testing.T) {
	truncateTables(t)
	user := User{Username: "hard-delete-user", Password: "password"}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}).Error)
	require.NoError(t, DB.Create(&TwoFABackupCode{UserId: user.Id, CodeHash: "hash"}).Error)
	require.NoError(t, DB.Create(&PasskeyCredential{UserID: user.Id, CredentialID: "credential", PublicKey: "public-key"}).Error)
	require.NoError(t, DB.Create(&UserOAuthBinding{UserId: user.Id, ProviderId: 1, ProviderUserId: "provider-user"}).Error)
	require.NoError(t, HardDeleteUserById(user.Id))

	var count int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
	for _, record := range []any{&TwoFA{}, &TwoFABackupCode{}, &PasskeyCredential{}, &UserOAuthBinding{}} {
		require.NoError(t, DB.Unscoped().Model(record).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.Zero(t, count)
	}
}

func TestIncrementFailedAttemptsCountsConcurrentFailures(t *testing.T) {
	truncateTables(t)
	user := User{Username: "twofa-cas-user", Password: "password"}
	require.NoError(t, DB.Create(&user).Error)
	twoFA := TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}
	require.NoError(t, DB.Create(&twoFA).Error)

	const attempts = 4
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- (&TwoFA{Id: twoFA.Id}).IncrementFailedAttempts() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var reloaded TwoFA
	require.NoError(t, DB.First(&reloaded, twoFA.Id).Error)
	assert.Equal(t, attempts, reloaded.FailedAttempts)
}

func TestValidateBackupCodeCanOnlySucceedOnce(t *testing.T) {
	truncateTables(t)
	const code = "ABCD-1234"
	require.NoError(t, CreateBackupCodes(123, []string{code}))
	const attempts = 2
	results := make(chan bool, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() { defer wg.Done(); valid, err := ValidateBackupCode(123, code); results <- valid; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	wins := 0
	for valid := range results {
		if valid {
			wins++
		}
	}
	assert.Equal(t, 1, wins)
	remaining, err := GetUnusedBackupCodeCount(123)
	require.NoError(t, err)
	assert.Zero(t, remaining)
}
