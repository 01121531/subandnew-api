package accountdataapi

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/stretchr/testify/require"
)

func TestPortalRateLimitIdentityDoesNotDependOnSession(t *testing.T) {
	identity := PortalRateLimitIdentity(42, "203.0.113.9")
	require.Equal(t, identity, PortalRateLimitIdentity(42, "203.0.113.9"))
	allowed, _ := AllowRequestKey(t.Context(), identity, 2)
	require.True(t, allowed)
	allowed, _ = AllowRequestKey(t.Context(), identity, 2)
	require.True(t, allowed)
	allowed, _ = AllowRequestKey(t.Context(), identity, 2)
	require.False(t, allowed)
}

func TestPortalLoginAttemptFallbackIsBounded(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	portalLoginAttempts.Lock()
	portalLoginAttempts.items = map[string]portalLoginAttempt{}
	portalLoginAttempts.Unlock()
	for index := 0; index < portalLoginLocalLimit+50; index++ {
		portalLoginFailed("slug", string(rune(index+1)))
	}
	portalLoginAttempts.Lock()
	count := len(portalLoginAttempts.items)
	portalLoginAttempts.Unlock()
	require.LessOrEqual(t, count, portalLoginLocalLimit)
}
