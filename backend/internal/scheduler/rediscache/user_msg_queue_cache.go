package rediscache

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	logger "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// Redis key 使用 providerID 作为 hash tag，同一提供商的键落在同一个 Redis Cluster slot。
// 格式: umq:{providerID}:lock / umq:{providerID}:last。
const (
	umqKeyPrefix  = "umq:"
	umqLockSuffix = ":lock" // STRING (requestID), PX lockTtlMs
	umqLastSuffix = ":last" // STRING (毫秒时间戳), EX 60s

	// 锁索引用来替代后台清理对 umq:*:lock 的全量 SCAN。
	// member 是 providerID，score 是锁预计过期的 Redis Unix 毫秒时间戳。
	umqLockIndexKey              = "umq:lock:index" // ZSET：member 为提供商 ID，score 为锁预计过期的 Unix 毫秒
	umqLockIndexCleanupBatchSize = 1000
)

var (
	// acquireLockScript 原子获取串行锁，支持同一请求重入。
	// 返回是否取得锁及 Redis 观测的到期毫秒数，获取失败时也返回到期时间，供 Go 侧回填索引。
	// 升级遗留、索引写失败或释放竞态造成的索引缺项，在下一次争锁时补回。
	// PTTL 为 -1 的锁返回当前时间，立即进入清理候选。
	acquireLockScript = redis.NewScript(`
redis.replicate_commands()
local cur = redis.call('GET', KEYS[1])
local ttl = tonumber(ARGV[2])
if cur == ARGV[1] then
    redis.call('PEXPIRE', KEYS[1], ttl)
    local t = redis.call('TIME')
    local ms = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
    return {1, ms + ttl}
end
if cur ~= false then
    local t = redis.call('TIME')
    local ms = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
    local pttl = redis.call('PTTL', KEYS[1])
    if pttl and pttl > 0 then
        return {0, ms + pttl}
    end
    return {0, ms}
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ttl)
local t = redis.call('TIME')
local ms = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
return {1, ms + ttl}
`)

	// releaseLockScript 原子释放锁，并使用 Redis TIME 记录完成时间。
	releaseLockScript = redis.NewScript(`
-- 兼容 3.2-4.x：脚本使用 TIME，需启用按效果复制，确保写入能同步到从库。
-- 5.0 及以上默认按效果复制；保留调用不改变行为。
redis.replicate_commands()
local cur = redis.call('GET', KEYS[1])
if cur == ARGV[1] then
    redis.call('DEL', KEYS[1])
    local t = redis.call('TIME')
    local ms = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
    redis.call('SET', KEYS[2], ms, 'EX', 60)
    return 1
end
return 0
`)

	// Lua 脚本：校验锁 TTL 状态，PTTL == -1 时原子删除异常锁。
	// 返回状态: -2=锁不存在，-1=无 TTL 的异常锁已删除，1=锁仍存活并返回剩余 PTTL。
	reconcileLockScript = redis.NewScript(`
local pttl = redis.call('PTTL', KEYS[1])
if pttl == -2 then
    return {-2, 0}
end
if pttl == -1 then
    redis.call('DEL', KEYS[1])
    return {-1, 0}
end
return {1, pttl}
`)
)

type userMsgQueueCache struct {
	rdb *redis.Client
}

// NewUserMsgQueueCache 创建用户消息队列缓存。
func NewUserMsgQueueCache(rdb *redis.Client) scheduler.UserMsgQueueCache {
	return &userMsgQueueCache{rdb: rdb}
}

func umqLockKey(providerID int64) string {
	// 格式为 umq:{123}:lock，花括号中的提供商 ID 是 Redis Cluster hash tag。
	return umqKeyPrefix + "{" + strconv.FormatInt(providerID, 10) + "}" + umqLockSuffix
}

func umqLastKey(providerID int64) string {
	// 格式为 umq:{123}:last，与 lockKey 位于同一个 hash slot。
	return umqKeyPrefix + "{" + strconv.FormatInt(providerID, 10) + "}" + umqLastSuffix
}

// AcquireLock 尝试获取提供商级串行锁
// 无论成功与否都尽力写入锁索引：成功时登记自己的锁，失败时回填观测到的持有者锁，
// 后台 reconcile 根据索引发现被争用的锁。
func (c *userMsgQueueCache) AcquireLock(ctx context.Context, providerID int64, requestID string, lockTtlMs int) (bool, error) {
	key := umqLockKey(providerID)
	result, err := acquireLockScript.Run(ctx, c.rdb, []string{key}, requestID, lockTtlMs).Result()
	if err != nil {
		return false, fmt.Errorf("umq acquire lock: %w", err)
	}
	acquired, err := redisScriptInt64At(result, 0)
	if err != nil {
		return false, fmt.Errorf("umq parse acquire lock result: %w", err)
	}
	expireAtMs, err := redisScriptInt64At(result, 1)
	if err != nil {
		return false, fmt.Errorf("umq parse acquire lock expire: %w", err)
	}
	if expireAtMs > 0 {
		if err := c.rdb.ZAdd(ctx, umqLockIndexKey, redis.Z{
			Score:  float64(expireAtMs),
			Member: strconv.FormatInt(providerID, 10),
		}).Err(); err != nil {
			logger.LegacyPrintf("repository.umq", "Warning: update lock index for provider %d failed: %v", providerID, err)
		}
	}
	return acquired == 1, nil
}

// ReleaseLock 释放锁并记录完成时间
// requestID 匹配时删除锁索引，其他请求写入的新锁继续保留。
func (c *userMsgQueueCache) ReleaseLock(ctx context.Context, providerID int64, requestID string) (bool, error) {
	lockKey := umqLockKey(providerID)
	lastKey := umqLastKey(providerID)
	result, err := releaseLockScript.Run(ctx, c.rdb, []string{lockKey, lastKey}, requestID).Int()
	if err != nil {
		return false, fmt.Errorf("umq release lock: %w", err)
	}
	if result == 1 {
		// 与下一个 AcquireLock 的 ZAdd 存在竞态：可能误删新持有者刚写入的索引项。
		// 该锁下次被争用时 AcquireLock 的回填路径会重新登记，无需在此加锁。
		if err := c.rdb.ZRem(ctx, umqLockIndexKey, strconv.FormatInt(providerID, 10)).Err(); err != nil {
			logger.LegacyPrintf("repository.umq", "Warning: remove lock index for provider %d failed: %v", providerID, err)
		}
	}
	return result == 1, nil
}

// GetLastCompletedMs 获取上次完成时间（毫秒时间戳）。
func (c *userMsgQueueCache) GetLastCompletedMs(ctx context.Context, providerID int64) (int64, error) {
	key := umqLastKey(providerID)
	val, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("umq get last completed: %w", err)
	}
	ms, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("umq parse last completed: %w", err)
	}
	return ms, nil
}

// GetCurrentTimeMs 返回 Redis TIME 的服务器时间（毫秒），与锁记录共用时间源。
func (c *userMsgQueueCache) GetCurrentTimeMs(ctx context.Context) (int64, error) {
	t, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return 0, fmt.Errorf("umq get redis time: %w", err)
	}
	return t.UnixMilli(), nil
}

// ReconcileExpiredLockCandidates 只处理索引里已经到期的候选锁。
// 候选到期不等于锁一定失效：可能是续租后索引滞后，所以必须再用 PTTL 二次确认。
func (c *userMsgQueueCache) ReconcileExpiredLockCandidates(ctx context.Context, maxCount int) (int, error) {
	if maxCount <= 0 {
		maxCount = umqLockIndexCleanupBatchSize
	}
	nowMs, err := c.GetCurrentTimeMs(ctx)
	if err != nil {
		return 0, err
	}
	members, err := c.rdb.ZRangeByScore(ctx, umqLockIndexKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatInt(nowMs, 10),
		Count: int64(maxCount),
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("umq read lock index: %w", err)
	}

	cleaned := 0
	for _, member := range members {
		providerID, err := strconv.ParseInt(member, 10, 64)
		if err != nil || providerID <= 0 {
			c.removeLockIndexMember(ctx, member)
			continue
		}

		result, err := reconcileLockScript.Run(ctx, c.rdb, []string{umqLockKey(providerID)}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return cleaned, fmt.Errorf("umq reconcile lock: %w", err)
		}
		status, err := redisScriptInt64At(result, 0)
		if err != nil {
			return cleaned, fmt.Errorf("umq parse reconcile status: %w", err)
		}
		pttl, err := redisScriptInt64At(result, 1)
		if err != nil {
			return cleaned, fmt.Errorf("umq parse reconcile pttl: %w", err)
		}

		switch status {
		case -2:
			// 锁自然过期或已释放，只需移除索引残留。
			c.removeLockIndexMember(ctx, member)
		case -1:
			// 无 TTL 的锁会永久阻塞队列，Lua 已原子删除它，这里统计一次清理。
			c.removeLockIndexMember(ctx, member)
			cleaned++
		case 1:
			// 锁仍存活，说明索引过期时间滞后；按剩余 PTTL 重新排期。
			if err := c.rdb.ZAdd(ctx, umqLockIndexKey, redis.Z{
				Score:  float64(nowMs + pttl),
				Member: member,
			}).Err(); err != nil {
				logger.LegacyPrintf("repository.umq", "Warning: reschedule lock index member %s failed: %v", member, err)
			}
		}
	}
	return cleaned, nil
}

// removeLockIndexMember 移除锁索引残留；索引维护是 best-effort，失败只记日志。
func (c *userMsgQueueCache) removeLockIndexMember(ctx context.Context, member string) {
	if err := c.rdb.ZRem(ctx, umqLockIndexKey, member).Err(); err != nil {
		logger.LegacyPrintf("repository.umq", "Warning: remove lock index member %s failed: %v", member, err)
	}
}

// redisScriptInt64At 兼容 go-redis 对 Lua 数组元素的不同返回类型。
func redisScriptInt64At(result any, index int) (int64, error) {
	values, ok := result.([]any)
	if !ok {
		return 0, fmt.Errorf("expected redis script array, got %T", result)
	}
	if index < 0 || index >= len(values) {
		return 0, fmt.Errorf("redis script array missing index %d", index)
	}
	switch v := values[index].(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis script value %T", v)
	}
}
