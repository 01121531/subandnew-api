package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

const temporaryCVVRedisPrefix = "subandnew:mailbox:temporary-cvv:v1:"

var (
	temporaryCVVRedisMu     sync.Mutex
	temporaryCVVRedisClient *redis.Client
	temporaryCVVRedisURL    string
	temporaryCVVRedisErr    error
)

var (
	errTemporaryCVVRedisMissing     = errors.New("temporary cvv redis is not configured")
	errTemporaryCVVRedisPersistence = errors.New("temporary cvv redis persistence must be disabled")
)

type temporaryCVVRedisValue struct {
	DeliveryID   string `json:"delivery_id"`
	Value        []byte `json:"value"`
	ExpiresAt    int64  `json:"expires_at"`
	AssignmentID int64  `json:"assignment_id"`
	OperatorID   int64  `json:"operator_id"`
	AuthVersion  int64  `json:"auth_version"`
}

func temporaryCVVRedisMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MAILBOX_TEMP_CVV_MODE")), "redis")
}

func temporaryCVVRedisErrorCode(err error) string {
	switch {
	case errors.Is(err, errTemporaryCVVRedisMissing):
		return "redis_not_configured"
	case errors.Is(err, errTemporaryCVVRedisPersistence):
		return "redis_persistence_enabled"
	default:
		return "redis_unavailable"
	}
}

func temporaryCVVRedis() (*redis.Client, error) {
	url := strings.TrimSpace(os.Getenv("MAILBOX_TEMP_CVV_REDIS_CONN_STRING"))
	if url == "" {
		return nil, errTemporaryCVVRedisMissing
	}
	temporaryCVVRedisMu.Lock()
	defer temporaryCVVRedisMu.Unlock()
	if temporaryCVVRedisURL == url && temporaryCVVRedisClient != nil {
		return temporaryCVVRedisClient, nil
	}
	if temporaryCVVRedisClient != nil {
		_ = temporaryCVVRedisClient.Close()
	}
	temporaryCVVRedisURL, temporaryCVVRedisClient, temporaryCVVRedisErr = url, nil, nil
	options, err := redis.ParseURL(url)
	if err != nil {
		temporaryCVVRedisErr = fmt.Errorf("parse temporary cvv redis: %w", err)
		return nil, temporaryCVVRedisErr
	}
	options.PoolSize = 5
	client := redis.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		temporaryCVVRedisErr = fmt.Errorf("ping temporary cvv redis: %w", err)
		return nil, temporaryCVVRedisErr
	}
	appendOnly, err := temporaryCVVRedisConfig(ctx, client, "appendonly")
	if err != nil {
		_ = client.Close()
		temporaryCVVRedisErr = err
		return nil, err
	}
	save, err := temporaryCVVRedisConfig(ctx, client, "save")
	if err != nil {
		_ = client.Close()
		temporaryCVVRedisErr = err
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(appendOnly), "no") || strings.TrimSpace(save) != "" {
		_ = client.Close()
		temporaryCVVRedisErr = errTemporaryCVVRedisPersistence
		return nil, temporaryCVVRedisErr
	}
	temporaryCVVRedisClient = client
	return client, nil
}

func temporaryCVVRedisConfig(ctx context.Context, client *redis.Client, name string) (string, error) {
	values, err := client.ConfigGet(ctx, name).Result()
	if err != nil {
		return "", fmt.Errorf("read temporary cvv redis configuration: %w", err)
	}
	if len(values) != 2 {
		return "", fmt.Errorf("read temporary cvv redis configuration: invalid %s response", name)
	}
	return fmt.Sprint(values[1]), nil
}

func validateTemporaryCVVRedis() error {
	_, err := temporaryCVVRedis()
	return err
}

func temporaryCVVRedisKey(id int64) string {
	return temporaryCVVRedisPrefix + strconv.FormatInt(id, 10)
}

func saveTemporaryCVVRedis(id int64, item *temporaryCVV, ttl time.Duration) error {
	if ttl <= 0 {
		return deleteTemporaryCVVRedis(id)
	}
	client, err := temporaryCVVRedis()
	if err != nil {
		return err
	}
	expiresAt := int64(0)
	if !item.expires.IsZero() {
		expiresAt = item.expires.Unix()
	}
	encoded, err := json.Marshal(temporaryCVVRedisValue{
		DeliveryID: item.deliveryID, Value: item.value, ExpiresAt: expiresAt,
		AssignmentID: item.assignmentID, OperatorID: item.operatorID, AuthVersion: item.authVersion,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return client.Set(ctx, temporaryCVVRedisKey(id), encoded, ttl).Err()
}

func loadTemporaryCVVRedis(id int64) (*temporaryCVV, error) {
	client, err := temporaryCVVRedis()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	encoded, err := client.Get(ctx, temporaryCVVRedisKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value temporaryCVVRedisValue
	if err := json.Unmarshal(encoded, &value); err != nil || value.DeliveryID == "" || !cvvPattern.Match(value.Value) {
		_ = client.Del(ctx, temporaryCVVRedisKey(id)).Err()
		return nil, fmt.Errorf("invalid temporary cvv redis value")
	}
	item := &temporaryCVV{
		deliveryID: value.DeliveryID, value: value.Value, assignmentID: value.AssignmentID,
		operatorID: value.OperatorID, authVersion: value.AuthVersion,
	}
	if value.ExpiresAt > 0 {
		item.expires = time.Unix(value.ExpiresAt, 0)
	}
	return item, nil
}

var claimTemporaryCVVScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return false end
local item = cjson.decode(raw)
if item.delivery_id ~= ARGV[1]
  or tonumber(item.assignment_id) ~= tonumber(ARGV[2])
  or tonumber(item.operator_id) ~= tonumber(ARGV[3])
  or tonumber(item.auth_version) ~= tonumber(ARGV[4]) then
  return 'mismatch'
end
if tonumber(item.expires_at) == 0 then
  item.expires_at = tonumber(ARGV[5]) + tonumber(ARGV[6])
  raw = cjson.encode(item)
  redis.call('SET', KEYS[1], raw, 'EX', tonumber(ARGV[6]))
end
return raw
`)

func claimTemporaryCVVRedis(id int64, expected *temporaryCVV, now time.Time) (*temporaryCVV, error) {
	client, err := temporaryCVVRedis()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := claimTemporaryCVVScript.Run(ctx, client, []string{temporaryCVVRedisKey(id)},
		expected.deliveryID, expected.assignmentID, expected.operatorID, expected.authVersion,
		now.Unix(), int64(temporaryCVVRevealTTL/time.Second)).Result()
	if errors.Is(err, redis.Nil) || result == nil || result == false {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	encoded, ok := result.(string)
	if !ok || encoded == "mismatch" {
		return nil, fmt.Errorf("temporary cvv changed")
	}
	var value temporaryCVVRedisValue
	if err := json.Unmarshal([]byte(encoded), &value); err != nil {
		return nil, err
	}
	return &temporaryCVV{
		deliveryID: value.DeliveryID, value: value.Value, expires: time.Unix(value.ExpiresAt, 0),
		assignmentID: value.AssignmentID, operatorID: value.OperatorID, authVersion: value.AuthVersion,
	}, nil
}

func deleteTemporaryCVVRedis(id int64) error {
	client, err := temporaryCVVRedis()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return client.Del(ctx, temporaryCVVRedisKey(id)).Err()
}

func temporaryCVVRedisIDs() ([]string, error) {
	client, err := temporaryCVVRedis()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	keys := make([]string, 0)
	var cursor uint64
	for {
		batch, next, err := client.Scan(ctx, cursor, temporaryCVVRedisPrefix+"*", 500).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}

func temporaryCVVCount(store *cvvStore) (int, error) {
	if !temporaryCVVRedisMode() {
		return len(store.items), nil
	}
	keys, err := temporaryCVVRedisIDs()
	return len(keys), err
}

func deleteTemporaryCVVRedisOperator(operatorID int64) error {
	keys, err := temporaryCVVRedisIDs()
	if err != nil {
		return err
	}
	for _, key := range keys {
		id, err := strconv.ParseInt(strings.TrimPrefix(key, temporaryCVVRedisPrefix), 10, 64)
		if err != nil {
			continue
		}
		item, err := loadTemporaryCVVRedis(id)
		if err != nil {
			return err
		}
		if item != nil && item.operatorID == operatorID {
			if err := deleteTemporaryCVVRedis(id); err != nil {
				return err
			}
		}
	}
	return nil
}
