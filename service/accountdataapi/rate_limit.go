package accountdataapi

import (
	"context"
	"fmt"
	"time"

	"github.com/01121531/subandnew-api/common"
)

var localRateLimiter common.InMemoryRateLimiter

func init() {
	localRateLimiter.Init(2 * time.Minute)
}

func AllowRequest(ctx context.Context, keyID int64, maximum int) (bool, int) {
	if keyID == 0 || maximum <= 0 {
		return false, 60
	}
	now := time.Now()
	retryAfter := 60 - now.Second()
	if retryAfter <= 0 {
		retryAfter = 1
	}
	key := fmt.Sprintf("accountDataAPI:%d:%s", keyID, now.UTC().Format("200601021504"))
	if common.RedisEnabled && common.RDB != nil {
		count, err := common.RDB.Incr(ctx, key).Result()
		if err != nil {
			return false, retryAfter
		}
		if count == 1 {
			_ = common.RDB.Expire(ctx, key, 2*time.Minute).Err()
		}
		return count <= int64(maximum), retryAfter
	}
	return localRateLimiter.Request(key, maximum, 60), retryAfter
}
