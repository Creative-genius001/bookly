package locks

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type RedisLocker struct {
	client *redis.Client
}

type Lock struct {
	key   string
	value string
	ttl   time.Duration
}

func NewRedisLocker(client *redis.Client) *RedisLocker {
	return &RedisLocker{client: client}
}

func (l *RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lock, error) {
	value := uuid.NewString()
	ok, err := l.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("resource is locked")
	}
	return &Lock{key: key, value: value, ttl: ttl}, nil
}

func (l *RedisLocker) Release(ctx context.Context, lock *Lock) error {
	if lock == nil {
		return nil
	}
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`
	return l.client.Eval(ctx, script, []string{lock.key}, lock.value).Err()
}
