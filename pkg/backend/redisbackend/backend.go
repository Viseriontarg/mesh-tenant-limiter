package redisbackend

import (
	"context"
	"fmt"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/redis/go-redis/v9"
)

var syncScript = redis.NewScript(`
local key = KEYS[1]
local seqkey = KEYS[2]
local window = tonumber(ARGV[1])
local count = tonumber(ARGV[2])

local t = redis.call('TIME')
local now = (tonumber(t[1]) * 1000) + math.floor(tonumber(t[2]) / 1000)
local cutoff = now - window

redis.call('ZREMRANGEBYSCORE', key, 0, cutoff)

local base = redis.call('INCRBY', seqkey, count)
for i = 0, count - 1 do
	local member = tostring(now) .. '-' .. tostring(base - count + i + 1)
	redis.call('ZADD', key, now, member)
end

redis.call('PEXPIRE', key, window)
redis.call('PEXPIRE', seqkey, window)

return {now, redis.call('ZCARD', key)}
`)

// Backend asynchronously flushes local admissions into Redis using the server clock.
type Backend struct {
	client redis.UniversalClient
	prefix string
}

// New creates a Redis-backed sync backend.
func New(client redis.UniversalClient, prefix string) *Backend {
	if prefix == "" {
		prefix = "mesh-tenant-limiter"
	}
	return &Backend{client: client, prefix: prefix}
}

// Sync appends a batch of locally admitted requests into the central sliding window log.
func (b *Backend) Sync(ctx context.Context, tenantID string, limit config.RateLimit, count int) error {
	if count <= 0 {
		return nil
	}

	key := fmt.Sprintf("%s:tenant:%s:window", b.prefix, tenantID)
	seqKey := fmt.Sprintf("%s:tenant:%s:seq", b.prefix, tenantID)
	if err := syncScript.Run(ctx, b.client, []string{key, seqKey}, limit.Window.Milliseconds(), count).Err(); err != nil {
		return fmt.Errorf("redis sync failed: %w", err)
	}
	return nil
}

var _ interface {
	Sync(context.Context, string, config.RateLimit, int) error
} = (*Backend)(nil)

var _ = time.Millisecond
