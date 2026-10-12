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

// 并发槽位使用 Redis 有序集合，每个提供商或用户对应一个键，成员为 requestID，分数为时间戳。
// ZCARD 以 O(1) 复杂度计数，ZREMRANGEBYSCORE 清理过期槽位。
// 计数在单次 Redis 调用中完成，逐请求建键后使用 SCAN 会增加网络往返和遍历开销。
const (
	// 并发槽位键前缀（有序集合）
	// 格式: concurrency:provider:{providerID}。
	providerSlotKeyPrefix = "concurrency:provider:"
	// 格式: concurrency:user:{userID}。
	userSlotKeyPrefix = "concurrency:user:"
	// 格式: concurrency:api_key:{apiKeyID}。
	apiKeySlotKeyPrefix       = "concurrency:api_key:"
	liveProviderSlotKeyPrefix = "concurrency:live:provider:"
	liveUserSlotKeyPrefix     = "concurrency:live:user:"
	liveAPIKeySlotKeyPrefix   = "concurrency:live:api_key:"
	// API Key 维度的客户端 WebSocket 入站租约使用较短 TTL，空闲连接不占用轮次槽位。
	openAIWSIngressLeaseKeyPrefix  = "concurrency:openai_ws_ingress:api_key:"
	openAIWSIngressLeaseTTLSeconds = 60
	liveLeaseTTLSeconds            = 60
	// 等待队列计数器格式: concurrency:wait:{userID}。
	waitQueueKeyPrefix = "concurrency:wait:"
	// 提供商级等待队列计数器格式: wait:provider:{providerID}。
	providerWaitKeyPrefix = "wait:provider:"

	// 默认槽位过期时间（分钟），可通过配置覆盖。
	defaultSlotTTLMinutes = 15

	// 活跃索引用来替代后台任务全量 SCAN 槽位键。
	// member 是提供商/用户 ID，score 是“预计仍需关注到”的 Redis Unix 秒时间戳。
	providerActiveIndexKey = "concurrency:provider:active_index" // ZSET：member 为提供商 ID，score 为预计过期的 Unix 秒
	userActiveIndexKey     = "concurrency:user:active_index"     // ZSET：member 为用户 ID，score 为预计过期的 Unix 秒

	// 后台清理按批处理索引候选，限制每次 Redis 操作的处理量。
	activeIndexCleanupBatchSize  = 1000
	activeIndexPipelineChunkSize = 500

	// 一次性迁移 marker：活跃索引机制上线前遗留的等待计数键无法被索引发现，
	// 且有流量时 TTL 会被不断刷新，必须清扫一次。marker 存在即代表已完成。
	legacyWaitSweepMarkerKey = "concurrency:startup:legacy_wait_sweep:v1"
)

var (
	// acquireScript 使用有序集合计数并在未达上限时添加槽位
	// Redis TIME 提供各实例共用的服务器时间。问题
	// KEYS[1] = 普通槽位键，KEYS[2] = 对应 Live 槽位键
	// ARGV[1] = maxConcurrency
	// ARGV[2] = TTL（秒）
	// ARGV[3] = requestID
	// 返回 {是否成功, Redis 当前秒}，Go 侧复用同一时间源写活跃索引，省去额外 TIME 往返。
	acquireScript = redis.NewScript(`
		-- 兼容 3.2-4.x：脚本使用 TIME，需启用按效果复制，确保写入能同步到从库。
		-- 5.0 及以上默认按效果复制；保留调用不改变行为。
		redis.replicate_commands()
		local key = KEYS[1]
		local liveKey = KEYS[2]
		local maxConcurrency = tonumber(ARGV[1])
		local ttl = tonumber(ARGV[2])
		local requestID = ARGV[3]

		-- 使用 Redis 服务器时间，确保多实例时钟一致
		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local expireBefore = now - ttl

		-- 清理过期槽位
		redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
		redis.call('ZREMRANGEBYSCORE', liveKey, '-inf', now - 60)

		-- 检查是否已存在（支持重试场景刷新时间戳）
		local exists = redis.call('ZSCORE', key, requestID)
		if exists ~= false then
			redis.call('ZADD', key, now, requestID)
			redis.call('EXPIRE', key, ttl)
			return {1, now}
		end

		-- 检查是否达到并发上限
		local count = redis.call('ZCARD', key) + redis.call('ZCARD', liveKey)
		if count < maxConcurrency then
			redis.call('ZADD', key, now, requestID)
			redis.call('EXPIRE', key, ttl)
			return {1, now}
		end

		return {0, now}
	`)

	// getCountScript 统计有序集合中的槽位数量并清理过期条目
	// 使用 Redis TIME 命令获取服务器时间
	// KEYS[1] = 普通槽位键，KEYS[2] = 对应 Live 槽位键
	// ARGV[1] = TTL（秒）。
	getCountScript = redis.NewScript(`
		-- 兼容 3.2-4.x：脚本使用 TIME，需启用按效果复制，确保写入能同步到从库。
		-- 5.0 及以上默认按效果复制；保留调用不改变行为。
		redis.replicate_commands()
		local key = KEYS[1]
		local liveKey = KEYS[2]
		local ttl = tonumber(ARGV[1])

		-- 使用 Redis 服务器时间
		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local expireBefore = now - ttl

		redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
		redis.call('ZREMRANGEBYSCORE', liveKey, '-inf', now - 60)
		return redis.call('ZCARD', key) + redis.call('ZCARD', liveKey)
	`)

	acquireLiveLeaseScript = redis.NewScript(`
		redis.replicate_commands()
		local providerRegular = KEYS[1]
		local providerLive = KEYS[2]
		local userRegular = KEYS[3]
		local userLive = KEYS[4]
		local apiLive = KEYS[5]
		local providerMax = tonumber(ARGV[1])
		local userMax = tonumber(ARGV[2])
		local ttl = tonumber(ARGV[3])
		local leaseID = ARGV[4]
		local replacing = tonumber(ARGV[5])
		local now = tonumber(redis.call('TIME')[1])
		local liveExpireBefore = now - ttl
		redis.call('ZREMRANGEBYSCORE', providerLive, '-inf', liveExpireBefore)
		redis.call('ZREMRANGEBYSCORE', userLive, '-inf', liveExpireBefore)
		redis.call('ZREMRANGEBYSCORE', apiLive, '-inf', liveExpireBefore)
		if redis.call('ZSCORE', providerLive, leaseID) ~= false then
			return 1
		end
		local providerCount = redis.call('ZCARD', providerRegular) + redis.call('ZCARD', providerLive)
		local userCount = redis.call('ZCARD', userRegular) + redis.call('ZCARD', userLive)
		local allowance = 0
		if replacing == 1 then allowance = 1 end
		if providerMax > 0 and providerCount >= providerMax + allowance then return 0 end
		if userMax > 0 and userCount >= userMax + allowance then return 0 end
		redis.call('ZADD', providerLive, now, leaseID)
		redis.call('ZADD', userLive, now, leaseID)
		redis.call('ZADD', apiLive, now, leaseID)
		redis.call('EXPIRE', providerLive, ttl)
		redis.call('EXPIRE', userLive, ttl)
		redis.call('EXPIRE', apiLive, ttl)
		return 1
	`)

	refreshLiveLeaseScript = redis.NewScript(`
		redis.replicate_commands()
		local ttl = tonumber(ARGV[1])
		local leaseID = ARGV[2]
		local now = tonumber(redis.call('TIME')[1])
		local expireBefore = now - ttl
		for _, key in ipairs(KEYS) do
			redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
			if redis.call('ZSCORE', key, leaseID) == false then return 0 end
		end
		for _, key in ipairs(KEYS) do
			redis.call('ZADD', key, now, leaseID)
			redis.call('EXPIRE', key, ttl)
		end
		return 1
	`)

	// trackSlotScript 登记用于统计的槽位。
	// KEYS[1] = 有序集合键
	// ARGV[1] = TTL（秒）
	// ARGV[2] = requestID.
	trackSlotScript = redis.NewScript(`
		-- 兼容 Redis 3.2-4.x：启用按效果复制，确保 TIME 调用正确同步。
		-- Redis 5.0 及以上默认按效果复制，因此该调用不改变行为。
		redis.replicate_commands()
		local key = KEYS[1]
		local ttl = tonumber(ARGV[1])
		local requestID = ARGV[2]

		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local expireBefore = now - ttl

		redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
		redis.call('ZADD', key, now, requestID)
		redis.call('EXPIRE', key, ttl)
		return 1
	`)

	// acquireOpenAIWSIngressLeaseScript 使用 Redis TIME 原子清理崩溃成员，并获取或刷新租约。
	acquireOpenAIWSIngressLeaseScript = redis.NewScript(`
		redis.replicate_commands()
		local key = KEYS[1]
		local maxConnections = tonumber(ARGV[1])
		local ttl = tonumber(ARGV[2])
		local leaseID = ARGV[3]
		local now = tonumber(redis.call('TIME')[1])
		local expireBefore = now - ttl
		redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
		if redis.call('ZSCORE', key, leaseID) ~= false then
			redis.call('ZADD', key, now, leaseID)
			redis.call('EXPIRE', key, ttl)
			return 1
		end
		if redis.call('ZCARD', key) < maxConnections then
			redis.call('ZADD', key, now, leaseID)
			redis.call('EXPIRE', key, ttl)
			return 1
		end
		return 0
	`)

	// refreshOpenAIWSIngressLeaseScript 在租约成员存在时续期。成员缺失时，进程需要终止本地连接，
	// 该连接已失去分布式并发名额。
	refreshOpenAIWSIngressLeaseScript = redis.NewScript(`
		redis.replicate_commands()
		local key = KEYS[1]
		local ttl = tonumber(ARGV[1])
		local leaseID = ARGV[2]
		local now = tonumber(redis.call('TIME')[1])
		local expireBefore = now - ttl
		redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
		if redis.call('ZSCORE', key, leaseID) == false then
			return 0
		end
		redis.call('ZADD', key, now, leaseID)
		redis.call('EXPIRE', key, ttl)
		return 1
	`)

	// incrementWaitScript - refreshes TTL on each increment to keep queue depth accurate
	// KEYS[1] = wait queue key
	// ARGV[1] = maxWait
	// ARGV[2] = TTL in seconds
	// 返回 {是否成功, Redis 当前秒}，供 Go 侧免额外 TIME 往返写活跃索引。
	incrementWaitScript = redis.NewScript(`
		-- Redis 3.2-4.x compat: opt into effects replication so redis.call('TIME')
		-- replicates correctly. No-op on Redis 5.0+ (effects replication is default).
		redis.replicate_commands()
		local current = redis.call('GET', KEYS[1])
		if current == false then
			current = 0
		else
			current = tonumber(current)
		end
		local now = tonumber(redis.call('TIME')[1])

		if current >= tonumber(ARGV[1]) then
			return {0, now}
		end

		redis.call('INCR', KEYS[1])

		-- Refresh TTL so long-running traffic doesn't expire active queue counters.
		redis.call('EXPIRE', KEYS[1], ARGV[2])

		return {1, now}
	`)

	// incrementProviderWaitScript - provider-level wait queue count (refresh TTL on each increment)
	// 返回值同 incrementWaitScript：{是否成功, Redis 当前秒}。
	incrementProviderWaitScript = redis.NewScript(`
		-- Redis 3.2-4.x compat: opt into effects replication so redis.call('TIME')
		-- replicates correctly. No-op on Redis 5.0+ (effects replication is default).
		redis.replicate_commands()
		local current = redis.call('GET', KEYS[1])
		if current == false then
			current = 0
		else
			current = tonumber(current)
		end
		local now = tonumber(redis.call('TIME')[1])

		if current >= tonumber(ARGV[1]) then
			return {0, now}
		end

		redis.call('INCR', KEYS[1])

		-- Refresh TTL so long-running traffic doesn't expire active queue counters.
		redis.call('EXPIRE', KEYS[1], ARGV[2])

		return {1, now}
	`)

	// decrementWaitScript - same as before.
	decrementWaitScript = redis.NewScript(`
			local current = redis.call('GET', KEYS[1])
			if current ~= false and tonumber(current) > 0 then
				redis.call('DECR', KEYS[1])
			end
			return 1
		`)

	// cleanupExpiredSlotsScript 清理单个提供商/用户有序集合中过期槽位
	// KEYS[1] = 有序集合键
	// ARGV[1] = TTL（秒）。
	cleanupExpiredSlotsScript = redis.NewScript(`
		-- 兼容 3.2-4.x：脚本使用 TIME，需启用按效果复制，确保写入能同步到从库。
		-- 5.0 及以上默认按效果复制；保留调用不改变行为。
		redis.replicate_commands()
		local key = KEYS[1]
		local ttl = tonumber(ARGV[1])
		local timeResult = redis.call('TIME')
		local now = tonumber(timeResult[1])
		local expireBefore = now - ttl
		redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
		if redis.call('ZCARD', key) == 0 then
			redis.call('DEL', key)
		else
			redis.call('EXPIRE', key, ttl)
		end
		return 1
	`)

	// startupCleanupSlotScript 清理单个槽位 key 中非当前进程前缀的成员，单键操作兼容 Redis Cluster。
	// KEYS[1] 是有序集合键，ARGV[1] 是当前进程前缀，ARGV[2] 是槽位 TTL。
	// 返回 {清除数量, 剩余成员数}，Go 侧据剩余数决定索引 member 去留，无需再回读槽位。
	startupCleanupSlotScript = redis.NewScript(`
		local key = KEYS[1]
		local activePrefix = ARGV[1]
		local slotTTL = tonumber(ARGV[2])
		local removed = 0
		local members = redis.call('ZRANGE', key, 0, -1)
		for _, member in ipairs(members) do
			if string.sub(member, 1, string.len(activePrefix)) ~= activePrefix then
				removed = removed + redis.call('ZREM', key, member)
			end
		end
		local remaining = redis.call('ZCARD', key)
		if remaining == 0 then
			redis.call('DEL', key)
		else
			redis.call('EXPIRE', key, slotTTL)
		end
		return {removed, remaining}
	`)

	providerSlotIndex = slotIndexSpec{indexKey: providerActiveIndexKey, slotKey: providerSlotKey, waitKey: providerWaitKey}
	userSlotIndex     = slotIndexSpec{indexKey: userActiveIndexKey, slotKey: userSlotKey, waitKey: waitQueueKey}
)

type concurrencyCache struct {
	rdb                 *redis.Client
	slotTTLSeconds      int // 槽位过期时间（秒）
	waitQueueTTLSeconds int // 等待队列过期时间（秒）
}

// slotIndexSpec 描述一个活跃索引及其对应的槽位/等待键构造方式。
// slotKey 和 waitKey 的函数签名相同，通过字段名区分用途。
type slotIndexSpec struct {
	indexKey string
	slotKey  func(int64) string
	waitKey  func(int64) string
}

type activeIndexLoad struct {
	id        int64
	member    string
	slotCount int
	waitCount int
}

// NewConcurrencyCache 创建并发控制缓存
// slotTTLMinutes: 槽位过期时间（分钟），0 或负数使用默认值 15 分钟
// waitQueueTTLSeconds: 等待队列过期时间（秒），0 或负数使用 slot TTL。
func NewConcurrencyCache(rdb *redis.Client, slotTTLMinutes int, waitQueueTTLSeconds int) scheduler.ConcurrencyCache {
	if slotTTLMinutes <= 0 {
		slotTTLMinutes = defaultSlotTTLMinutes
	}
	if waitQueueTTLSeconds <= 0 {
		waitQueueTTLSeconds = slotTTLMinutes * 60
	}
	return &concurrencyCache{
		rdb:                 rdb,
		slotTTLSeconds:      slotTTLMinutes * 60,
		waitQueueTTLSeconds: waitQueueTTLSeconds,
	}
}

// providerSlotKey 生成提供商并发槽位的 Redis 键。
func providerSlotKey(providerID int64) string {
	return fmt.Sprintf("%s%d", providerSlotKeyPrefix, providerID)
}

func userSlotKey(userID int64) string {
	return fmt.Sprintf("%s%d", userSlotKeyPrefix, userID)
}

func apiKeySlotKey(apiKeyID int64) string {
	return fmt.Sprintf("%s%d", apiKeySlotKeyPrefix, apiKeyID)
}

func liveProviderSlotKey(providerID int64) string {
	return fmt.Sprintf("%s%d", liveProviderSlotKeyPrefix, providerID)
}

func liveUserSlotKey(userID int64) string {
	return fmt.Sprintf("%s%d", liveUserSlotKeyPrefix, userID)
}

func liveAPIKeySlotKey(apiKeyID int64) string {
	return fmt.Sprintf("%s%d", liveAPIKeySlotKeyPrefix, apiKeyID)
}

func openAIWSIngressLeaseKey(apiKeyID int64) string {
	return fmt.Sprintf("%s%d", openAIWSIngressLeaseKeyPrefix, apiKeyID)
}

func waitQueueKey(userID int64) string {
	return fmt.Sprintf("%s%d", waitQueueKeyPrefix, userID)
}

func providerWaitKey(providerID int64) string {
	return fmt.Sprintf("%s%d", providerWaitKeyPrefix, providerID)
}

// redisUnixSeconds 返回 Redis 服务器时间，各实例据此计算索引到期时间。
func (c *concurrencyCache) redisUnixSeconds(ctx context.Context) (int64, error) {
	now, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return 0, fmt.Errorf("redis TIME: %w", err)
	}
	return now.Unix(), nil
}

// touchActiveIndexAt 是写路径上的轻量标记：主操作已成功时，尽力把 ID 放入活跃索引，
// score 为给定的绝对过期时间（Redis Unix 秒）。索引失败不影响并发槽位/等待队列本身，
// 后续释放或清理会再次校正，因此只记日志不上抛。
func (c *concurrencyCache) touchActiveIndexAt(ctx context.Context, indexKey string, id int64, expireAt int64) {
	if c == nil || c.rdb == nil || id <= 0 || expireAt <= 0 {
		return
	}
	if err := c.rdb.ZAdd(ctx, indexKey, redis.Z{
		Score:  float64(expireAt),
		Member: strconv.FormatInt(id, 10),
	}).Err(); err != nil {
		logger.LegacyPrintf("repository.concurrency", "Warning: touch active index %s for %d failed: %v", indexKey, id, err)
	}
}

func (c *concurrencyCache) refreshProviderActiveIndex(ctx context.Context, providerID int64) {
	c.refreshActiveIndex(ctx, providerActiveIndexKey, providerID, providerSlotKey(providerID), providerWaitKey(providerID))
}

func (c *concurrencyCache) refreshUserActiveIndex(ctx context.Context, userID int64) {
	c.refreshActiveIndex(ctx, userActiveIndexKey, userID, userSlotKey(userID), waitQueueKey(userID))
}

// refreshActiveIndex 根据 Redis 中的槽位和等待计数重建索引状态。
// 释放槽位、等待计数减少、清理过期成员后都会调用它，防止索引残留。
// 索引维护失败时记录日志，调用方继续执行。
func (c *concurrencyCache) refreshActiveIndex(ctx context.Context, indexKey string, id int64, slotKey, waitKey string) {
	if c == nil || c.rdb == nil || id <= 0 {
		return
	}
	now, err := c.redisUnixSeconds(ctx)
	if err != nil {
		logger.LegacyPrintf("repository.concurrency", "Warning: refresh active index %s for %d failed: %v", indexKey, id, err)
		return
	}

	load, err := c.readActiveLoadForKey(ctx, id, slotKey, waitKey, now)
	if err != nil {
		logger.LegacyPrintf("repository.concurrency", "Warning: refresh active index %s for %d failed: %v", indexKey, id, err)
		return
	}
	member := strconv.FormatInt(id, 10)
	if load.slotCount == 0 && load.waitCount <= 0 {
		if err := c.rdb.ZRem(ctx, indexKey, member).Err(); err != nil {
			logger.LegacyPrintf("repository.concurrency", "Warning: remove active index member %s from %s failed: %v", member, indexKey, err)
		}
		return
	}

	ttlSeconds := c.activeIndexTTL(load.slotCount, load.waitCount)
	if ttlSeconds <= 0 {
		return
	}
	c.touchActiveIndexAt(ctx, indexKey, id, now+int64(ttlSeconds))
}

// activeIndexTTL 取槽位 TTL 与等待队列 TTL 中仍然需要关注的较大值。
// 只要并发槽位或等待计数还有负载，就保留索引；两者都为 0 时调用方会删除索引。
func (c *concurrencyCache) activeIndexTTL(slotCount int, waitCount int) int {
	ttlSeconds := 0
	if slotCount > 0 {
		ttlSeconds = c.slotTTLSeconds
	}
	if waitCount > 0 && c.waitQueueTTLSeconds > ttlSeconds {
		ttlSeconds = c.waitQueueTTLSeconds
	}
	return ttlSeconds
}

// readActiveLoadForKey 读取单个 ID 的当前负载，并顺手清理该槽位集合中的过期成员。
func (c *concurrencyCache) readActiveLoadForKey(ctx context.Context, id int64, slotKey, waitKey string, now int64) (activeIndexLoad, error) {
	cutoffTime := now - int64(c.slotTTLSeconds)
	pipe := c.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, slotKey, "-inf", strconv.FormatInt(cutoffTime, 10))
	zcardCmd := pipe.ZCard(ctx, slotKey)
	getCmd := pipe.Get(ctx, waitKey)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return activeIndexLoad{}, fmt.Errorf("pipeline exec: %w", err)
	}

	waitCount := 0
	if v, err := getCmd.Int(); err == nil && v > 0 {
		waitCount = v
	}
	return activeIndexLoad{
		id:        id,
		member:    strconv.FormatInt(id, 10),
		slotCount: int(zcardCmd.Val()),
		waitCount: waitCount,
	}, nil
}

// readIndexLoads 批量读取提供商或用户索引候选的负载。
// Pipeline 按块发送，在合并 Redis 往返的同时限制每次发送的命令数。
func (c *concurrencyCache) readIndexLoads(ctx context.Context, spec slotIndexSpec, members []string, now int64) ([]activeIndexLoad, []string, error) {
	loads := make([]activeIndexLoad, 0, len(members))
	staleMembers := make([]string, 0)
	candidates := make([]activeIndexLoad, 0, len(members))
	for _, member := range members {
		id, err := strconv.ParseInt(member, 10, 64)
		if err != nil || id <= 0 {
			staleMembers = append(staleMembers, member)
			continue
		}
		candidates = append(candidates, activeIndexLoad{id: id, member: member})
	}

	cutoffTime := now - int64(c.slotTTLSeconds)
	for start := 0; start < len(candidates); start += activeIndexPipelineChunkSize {
		end := min(start+activeIndexPipelineChunkSize, len(candidates))
		chunk := candidates[start:end]

		pipe := c.rdb.Pipeline()
		type loadCmd struct {
			activeIndexLoad
			zcardCmd *redis.IntCmd
			getCmd   *redis.StringCmd
		}
		cmds := make([]loadCmd, 0, len(chunk))
		for _, candidate := range chunk {
			slotKey := spec.slotKey(candidate.id)
			waitKey := spec.waitKey(candidate.id)
			pipe.ZRemRangeByScore(ctx, slotKey, "-inf", strconv.FormatInt(cutoffTime, 10))
			cmds = append(cmds, loadCmd{
				activeIndexLoad: candidate,
				zcardCmd:        pipe.ZCard(ctx, slotKey),
				getCmd:          pipe.Get(ctx, waitKey),
			})
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
			return nil, nil, fmt.Errorf("pipeline exec: %w", err)
		}
		for _, cmd := range cmds {
			waitCount := 0
			if v, err := cmd.getCmd.Int(); err == nil && v > 0 {
				waitCount = v
			}
			loads = append(loads, activeIndexLoad{
				id:        cmd.id,
				member:    cmd.member,
				slotCount: int(cmd.zcardCmd.Val()),
				waitCount: waitCount,
			})
		}
	}

	return loads, staleMembers, nil
}

// removeActiveIndexMembers 清理无效 member；这是辅助索引的维护动作，调用方无需因为失败中断主流程。
func (c *concurrencyCache) removeActiveIndexMembers(ctx context.Context, indexKey string, members []string) {
	if len(members) == 0 {
		return
	}
	args := make([]any, 0, len(members))
	for _, member := range members {
		args = append(args, member)
	}
	if err := c.rdb.ZRem(ctx, indexKey, args...).Err(); err != nil {
		logger.LegacyPrintf("repository.concurrency", "Warning: remove %d active index members from %s failed: %v", len(members), indexKey, err)
	}
}

// runScriptInt64Pair 执行返回两元素整数数组的 Lua 脚本并解析（如 {result, now}、{removed, remaining}）。
func runScriptInt64Pair(ctx context.Context, rdb *redis.Client, script *redis.Script, keys []string, args ...any) (int64, int64, error) {
	raw, err := script.Run(ctx, rdb, keys, args...).Result()
	if err != nil {
		return 0, 0, err
	}
	first, err := redisScriptInt64At(raw, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("parse script value 0: %w", err)
	}
	second, err := redisScriptInt64At(raw, 1)
	if err != nil {
		return 0, 0, fmt.Errorf("parse script value 1: %w", err)
	}
	return first, second, nil
}

// Provider slot operations

func (c *concurrencyCache) AcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int, requestID string) (bool, error) {
	key := providerSlotKey(providerID)
	// Lua 脚本从 Redis TIME 取得时间戳，各实例共用该时间源。
	result, now, err := runScriptInt64Pair(ctx, c.rdb, acquireScript, []string{key, liveProviderSlotKey(providerID)}, maxConcurrency, c.slotTTLSeconds, requestID)
	if err != nil {
		return false, err
	}
	if result == 1 {
		// 成功占槽后标记活跃提供商，后台清理即可从索引定位候选提供商。
		c.touchActiveIndexAt(ctx, providerActiveIndexKey, providerID, now+int64(c.slotTTLSeconds))
	}
	return result == 1, nil
}

func (c *concurrencyCache) ReleaseProviderSlot(ctx context.Context, providerID int64, requestID string) error {
	key := providerSlotKey(providerID)
	if err := c.rdb.ZRem(ctx, key, requestID).Err(); err != nil {
		return err
	}
	// 释放后根据槽位和等待计数刷新索引，两项都为空时移除 member。
	c.refreshProviderActiveIndex(ctx, providerID)
	return nil
}

func (c *concurrencyCache) GetProviderConcurrency(ctx context.Context, providerID int64) (int, error) {
	key := providerSlotKey(providerID)
	// 时间戳在 Lua 脚本内使用 Redis TIME 命令获取
	result, err := getCountScript.Run(ctx, c.rdb, []string{key, liveProviderSlotKey(providerID)}, c.slotTTLSeconds).Int()
	if err != nil {
		return 0, err
	}
	return result, nil
}

func (c *concurrencyCache) GetProviderConcurrencyBatch(ctx context.Context, providerIDs []int64) (map[int64]int, error) {
	if len(providerIDs) == 0 {
		return map[int64]int{}, nil
	}

	now, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis TIME: %w", err)
	}
	cutoffTime := now.Unix() - int64(c.slotTTLSeconds)

	pipe := c.rdb.Pipeline()
	type providerCmd struct {
		providerID int64
		zcardCmd   *redis.IntCmd
		liveCmd    *redis.IntCmd
	}
	cmds := make([]providerCmd, 0, len(providerIDs))
	for _, providerID := range providerIDs {
		slotKey := providerSlotKeyPrefix + strconv.FormatInt(providerID, 10)
		liveKey := liveProviderSlotKeyPrefix + strconv.FormatInt(providerID, 10)
		pipe.ZRemRangeByScore(ctx, slotKey, "-inf", strconv.FormatInt(cutoffTime, 10))
		pipe.ZRemRangeByScore(ctx, liveKey, "-inf", strconv.FormatInt(now.Unix()-liveLeaseTTLSeconds, 10))
		cmds = append(cmds, providerCmd{
			providerID: providerID,
			zcardCmd:   pipe.ZCard(ctx, slotKey),
			liveCmd:    pipe.ZCard(ctx, liveKey),
		})
	}

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pipeline exec: %w", err)
	}

	result := make(map[int64]int, len(providerIDs))
	for _, cmd := range cmds {
		result[cmd.providerID] = int(cmd.zcardCmd.Val() + cmd.liveCmd.Val())
	}
	return result, nil
}

// User slot operations

func (c *concurrencyCache) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
	key := userSlotKey(userID)
	// Lua 脚本从 Redis TIME 取得时间戳，各实例共用该时间源。
	result, now, err := runScriptInt64Pair(ctx, c.rdb, acquireScript, []string{key, liveUserSlotKey(userID)}, maxConcurrency, c.slotTTLSeconds, requestID)
	if err != nil {
		return false, err
	}
	if result == 1 {
		// 成功占槽后将用户加入活跃索引，启动清理从该索引读取候选。
		c.touchActiveIndexAt(ctx, userActiveIndexKey, userID, now+int64(c.slotTTLSeconds))
	}
	return result == 1, nil
}

func (c *concurrencyCache) ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error {
	key := userSlotKey(userID)
	if err := c.rdb.ZRem(ctx, key, requestID).Err(); err != nil {
		return err
	}
	// 释放后按 Redis 中剩余负载修正索引状态。
	c.refreshUserActiveIndex(ctx, userID)
	return nil
}

func (c *concurrencyCache) GetUserConcurrency(ctx context.Context, userID int64) (int, error) {
	key := userSlotKey(userID)
	// 时间戳在 Lua 脚本内使用 Redis TIME 命令获取
	result, err := getCountScript.Run(ctx, c.rdb, []string{key, liveUserSlotKey(userID)}, c.slotTTLSeconds).Int()
	if err != nil {
		return 0, err
	}
	return result, nil
}

// TrackAPIKeySlot 记录 API Key 的统计槽位。
func (c *concurrencyCache) TrackAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error {
	key := apiKeySlotKey(apiKeyID)
	_, err := trackSlotScript.Run(ctx, c.rdb, []string{key}, c.slotTTLSeconds, requestID).Result()
	return err
}

// ReleaseAPIKeySlot 释放 API Key 的统计槽位。
func (c *concurrencyCache) ReleaseAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error {
	key := apiKeySlotKey(apiKeyID)
	return c.rdb.ZRem(ctx, key, requestID).Err()
}

func (c *concurrencyCache) AcquireOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, maxConnections int, leaseID string) (bool, error) {
	if c == nil || c.rdb == nil || apiKeyID <= 0 || maxConnections <= 0 || leaseID == "" {
		return false, nil
	}
	result, err := acquireOpenAIWSIngressLeaseScript.Run(
		ctx,
		c.rdb,
		[]string{openAIWSIngressLeaseKey(apiKeyID)},
		maxConnections,
		openAIWSIngressLeaseTTLSeconds,
		leaseID,
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (c *concurrencyCache) RefreshOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, leaseID string) (bool, error) {
	if c == nil || c.rdb == nil || apiKeyID <= 0 || leaseID == "" {
		return false, nil
	}
	result, err := refreshOpenAIWSIngressLeaseScript.Run(
		ctx,
		c.rdb,
		[]string{openAIWSIngressLeaseKey(apiKeyID)},
		openAIWSIngressLeaseTTLSeconds,
		leaseID,
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (c *concurrencyCache) ReleaseOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, leaseID string) error {
	if c == nil || c.rdb == nil || apiKeyID <= 0 || leaseID == "" {
		return nil
	}
	return c.rdb.ZRem(ctx, openAIWSIngressLeaseKey(apiKeyID), leaseID).Err()
}

func (c *concurrencyCache) AcquireLiveLease(
	ctx context.Context,
	providerID int64,
	providerMax int,
	userID int64,
	userMax int,
	apiKeyID int64,
	leaseID string,
	replacingRegularSlots bool,
) (bool, error) {
	if c == nil || c.rdb == nil || providerID <= 0 || userID <= 0 || apiKeyID <= 0 || leaseID == "" {
		return false, nil
	}
	replacing := 0
	if replacingRegularSlots {
		replacing = 1
	}
	result, err := acquireLiveLeaseScript.Run(ctx, c.rdb, []string{
		providerSlotKey(providerID),
		liveProviderSlotKey(providerID),
		userSlotKey(userID),
		liveUserSlotKey(userID),
		liveAPIKeySlotKey(apiKeyID),
	}, providerMax, userMax, liveLeaseTTLSeconds, leaseID, replacing).Int()
	return result == 1, err
}

func (c *concurrencyCache) RefreshLiveLease(ctx context.Context, providerID, userID, apiKeyID int64, leaseID string) (bool, error) {
	if c == nil || c.rdb == nil || leaseID == "" {
		return false, nil
	}
	result, err := refreshLiveLeaseScript.Run(ctx, c.rdb, []string{
		liveProviderSlotKey(providerID),
		liveUserSlotKey(userID),
		liveAPIKeySlotKey(apiKeyID),
	}, liveLeaseTTLSeconds, leaseID).Int()
	return result == 1, err
}

func (c *concurrencyCache) ReleaseLiveLease(ctx context.Context, providerID, userID, apiKeyID int64, leaseID string) error {
	if c == nil || c.rdb == nil || leaseID == "" {
		return nil
	}
	pipe := c.rdb.TxPipeline()
	pipe.ZRem(ctx, liveProviderSlotKey(providerID), leaseID)
	pipe.ZRem(ctx, liveUserSlotKey(userID), leaseID)
	pipe.ZRem(ctx, liveAPIKeySlotKey(apiKeyID), leaseID)
	_, err := pipe.Exec(ctx)
	return err
}

// GetAPIKeyConcurrencyBatch 批量读取 API Key 的实时槽位数。
func (c *concurrencyCache) GetAPIKeyConcurrencyBatch(ctx context.Context, apiKeyIDs []int64) (map[int64]int, error) {
	if len(apiKeyIDs) == 0 {
		return map[int64]int{}, nil
	}

	now, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis TIME: %w", err)
	}
	cutoffTime := now.Unix() - int64(c.slotTTLSeconds)

	pipe := c.rdb.Pipeline()
	type apiKeyCmd struct {
		apiKeyID int64
		zcardCmd *redis.IntCmd
		liveCmd  *redis.IntCmd
	}
	cmds := make([]apiKeyCmd, 0, len(apiKeyIDs))
	for _, apiKeyID := range apiKeyIDs {
		slotKey := apiKeySlotKeyPrefix + strconv.FormatInt(apiKeyID, 10)
		liveKey := liveAPIKeySlotKeyPrefix + strconv.FormatInt(apiKeyID, 10)
		pipe.ZRemRangeByScore(ctx, slotKey, "-inf", strconv.FormatInt(cutoffTime, 10))
		pipe.ZRemRangeByScore(ctx, liveKey, "-inf", strconv.FormatInt(now.Unix()-liveLeaseTTLSeconds, 10))
		cmds = append(cmds, apiKeyCmd{
			apiKeyID: apiKeyID,
			zcardCmd: pipe.ZCard(ctx, slotKey),
			liveCmd:  pipe.ZCard(ctx, liveKey),
		})
	}

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pipeline exec: %w", err)
	}

	result := make(map[int64]int, len(apiKeyIDs))
	for _, cmd := range cmds {
		result[cmd.apiKeyID] = int(cmd.zcardCmd.Val() + cmd.liveCmd.Val())
	}
	return result, nil
}

// Wait queue operations

func (c *concurrencyCache) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	key := waitQueueKey(userID)
	result, now, err := runScriptInt64Pair(ctx, c.rdb, incrementWaitScript, []string{key}, maxWait, c.waitQueueTTLSeconds)
	if err != nil {
		return false, err
	}
	if result == 1 {
		// 等待队列也会让用户保持“活跃”，否则槽位为 0 时后台任务可能漏看等待计数。
		c.touchActiveIndexAt(ctx, userActiveIndexKey, userID, now+int64(c.waitQueueTTLSeconds))
	}
	return result == 1, nil
}

func (c *concurrencyCache) DecrementWaitCount(ctx context.Context, userID int64) error {
	key := waitQueueKey(userID)
	_, err := decrementWaitScript.Run(ctx, c.rdb, []string{key}).Result()
	if err == nil {
		// 等待数减少后重新判断是否还需要保留索引。
		c.refreshUserActiveIndex(ctx, userID)
	}
	return err
}

// Provider wait queue operations

func (c *concurrencyCache) IncrementProviderWaitCount(ctx context.Context, providerID int64, maxWait int) (bool, error) {
	key := providerWaitKey(providerID)
	result, now, err := runScriptInt64Pair(ctx, c.rdb, incrementProviderWaitScript, []string{key}, maxWait, c.waitQueueTTLSeconds)
	if err != nil {
		return false, err
	}
	if result == 1 {
		// 提供商级等待队列同样写入提供商活跃索引，供负载查询和清理任务使用。
		c.touchActiveIndexAt(ctx, providerActiveIndexKey, providerID, now+int64(c.waitQueueTTLSeconds))
	}
	return result == 1, nil
}

func (c *concurrencyCache) DecrementProviderWaitCount(ctx context.Context, providerID int64) error {
	key := providerWaitKey(providerID)
	_, err := decrementWaitScript.Run(ctx, c.rdb, []string{key}).Result()
	if err == nil {
		// 等待计数归零后删除索引项，后台任务据此跳过已空闲的提供商。
		c.refreshProviderActiveIndex(ctx, providerID)
	}
	return err
}

func (c *concurrencyCache) GetProviderWaitingCount(ctx context.Context, providerID int64) (int, error) {
	key := providerWaitKey(providerID)
	val, err := c.rdb.Get(ctx, key).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, err
	}
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return val, nil
}

func (c *concurrencyCache) GetProvidersLoadBatch(ctx context.Context, providers []scheduler.ProviderWithConcurrency) (map[int64]*scheduler.ProviderLoadInfo, error) {
	if len(providers) == 0 {
		return map[int64]*scheduler.ProviderLoadInfo{}, nil
	}

	// 使用 Pipeline 替代 Lua 脚本，兼容 Redis Cluster（Lua 内动态拼 key 会 CROSSSLOT）。
	// 每个提供商执行 3 个命令：ZREMRANGEBYSCORE（清理过期）、ZCARD（并发数）、GET（等待数）。
	now, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis TIME: %w", err)
	}
	cutoffTime := now.Unix() - int64(c.slotTTLSeconds)

	pipe := c.rdb.Pipeline()

	type providerCmds struct {
		id             int64
		maxConcurrency int
		zcardCmd       *redis.IntCmd
		liveCmd        *redis.IntCmd
		getCmd         *redis.StringCmd
	}
	cmds := make([]providerCmds, 0, len(providers))
	for _, acc := range providers {
		slotKey := providerSlotKeyPrefix + strconv.FormatInt(acc.ID, 10)
		liveKey := liveProviderSlotKeyPrefix + strconv.FormatInt(acc.ID, 10)
		waitKey := providerWaitKeyPrefix + strconv.FormatInt(acc.ID, 10)
		pipe.ZRemRangeByScore(ctx, slotKey, "-inf", strconv.FormatInt(cutoffTime, 10))
		pipe.ZRemRangeByScore(ctx, liveKey, "-inf", strconv.FormatInt(now.Unix()-liveLeaseTTLSeconds, 10))
		ac := providerCmds{
			id:             acc.ID,
			maxConcurrency: acc.MaxConcurrency,
			zcardCmd:       pipe.ZCard(ctx, slotKey),
			liveCmd:        pipe.ZCard(ctx, liveKey),
			getCmd:         pipe.Get(ctx, waitKey),
		}
		cmds = append(cmds, ac)
	}

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pipeline exec: %w", err)
	}

	loadMap := make(map[int64]*scheduler.ProviderLoadInfo, len(providers))
	for _, ac := range cmds {
		currentConcurrency := int(ac.zcardCmd.Val() + ac.liveCmd.Val())
		waitingCount := 0
		if v, err := ac.getCmd.Int(); err == nil {
			waitingCount = v
		}
		loadRate := 0
		if ac.maxConcurrency > 0 {
			loadRate = (currentConcurrency + waitingCount) * 100 / ac.maxConcurrency
		}
		loadMap[ac.id] = &scheduler.ProviderLoadInfo{
			ProviderID:         ac.id,
			CurrentConcurrency: currentConcurrency,
			WaitingCount:       waitingCount,
			LoadRate:           loadRate,
		}
	}

	return loadMap, nil
}

func (c *concurrencyCache) GetUsersLoadBatch(ctx context.Context, users []scheduler.UserWithConcurrency) (map[int64]*scheduler.UserLoadInfo, error) {
	if len(users) == 0 {
		return map[int64]*scheduler.UserLoadInfo{}, nil
	}

	// 使用 Pipeline 替代 Lua 脚本，兼容 Redis Cluster。
	now, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis TIME: %w", err)
	}
	cutoffTime := now.Unix() - int64(c.slotTTLSeconds)

	pipe := c.rdb.Pipeline()

	type userCmds struct {
		id             int64
		maxConcurrency int
		zcardCmd       *redis.IntCmd
		liveCmd        *redis.IntCmd
		getCmd         *redis.StringCmd
	}
	cmds := make([]userCmds, 0, len(users))
	for _, u := range users {
		slotKey := userSlotKeyPrefix + strconv.FormatInt(u.ID, 10)
		liveKey := liveUserSlotKeyPrefix + strconv.FormatInt(u.ID, 10)
		waitKey := waitQueueKeyPrefix + strconv.FormatInt(u.ID, 10)
		pipe.ZRemRangeByScore(ctx, slotKey, "-inf", strconv.FormatInt(cutoffTime, 10))
		pipe.ZRemRangeByScore(ctx, liveKey, "-inf", strconv.FormatInt(now.Unix()-liveLeaseTTLSeconds, 10))
		uc := userCmds{
			id:             u.ID,
			maxConcurrency: u.MaxConcurrency,
			zcardCmd:       pipe.ZCard(ctx, slotKey),
			liveCmd:        pipe.ZCard(ctx, liveKey),
			getCmd:         pipe.Get(ctx, waitKey),
		}
		cmds = append(cmds, uc)
	}

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pipeline exec: %w", err)
	}

	loadMap := make(map[int64]*scheduler.UserLoadInfo, len(users))
	for _, uc := range cmds {
		currentConcurrency := int(uc.zcardCmd.Val() + uc.liveCmd.Val())
		waitingCount := 0
		if v, err := uc.getCmd.Int(); err == nil {
			waitingCount = v
		}
		loadRate := 0
		if uc.maxConcurrency > 0 {
			loadRate = (currentConcurrency + waitingCount) * 100 / uc.maxConcurrency
		}
		loadMap[uc.id] = &scheduler.UserLoadInfo{
			UserID:             uc.id,
			CurrentConcurrency: currentConcurrency,
			WaitingCount:       waitingCount,
			LoadRate:           loadRate,
		}
	}

	return loadMap, nil
}

func (c *concurrencyCache) CleanupExpiredProviderSlots(ctx context.Context, providerID int64) error {
	key := providerSlotKey(providerID)
	_, err := cleanupExpiredSlotsScript.Run(ctx, c.rdb, []string{key}, c.slotTTLSeconds).Result()
	if err == nil {
		// 单提供商清理后同步索引，保持后台批量清理的候选集准确。
		c.refreshProviderActiveIndex(ctx, providerID)
	}
	return err
}

// CleanupExpiredProviderSlotKeys 处理提供商和用户两个活跃索引中已到期的候选。
func (c *concurrencyCache) CleanupExpiredProviderSlotKeys(ctx context.Context) error {
	if err := c.reconcileExpiredIndexCandidates(ctx, providerSlotIndex); err != nil {
		return err
	}
	return c.reconcileExpiredIndexCandidates(ctx, userSlotIndex)
}

// reconcileExpiredIndexCandidates 处理单个活跃索引中 score 已到期的候选：
// 负载为空时移除 member，仍有负载时按当前计数批量刷新 score。
func (c *concurrencyCache) reconcileExpiredIndexCandidates(ctx context.Context, spec slotIndexSpec) error {
	now, err := c.redisUnixSeconds(ctx)
	if err != nil {
		return err
	}
	members, err := c.rdb.ZRangeByScore(ctx, spec.indexKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatInt(now, 10),
		Count: activeIndexCleanupBatchSize,
	}).Result()
	if err != nil {
		return fmt.Errorf("read expired index %s: %w", spec.indexKey, err)
	}

	loads, staleMembers, err := c.readIndexLoads(ctx, spec, members, now)
	if err != nil {
		return err
	}
	refreshed := make([]redis.Z, 0, len(loads))
	for _, load := range loads {
		if load.slotCount == 0 && load.waitCount <= 0 {
			// 槽位和等待计数均为空，可删除该索引成员。
			staleMembers = append(staleMembers, load.member)
			continue
		}
		refreshed = append(refreshed, redis.Z{
			Score:  float64(now + int64(c.activeIndexTTL(load.slotCount, load.waitCount))),
			Member: load.member,
		})
	}
	if len(refreshed) > 0 {
		if err := c.rdb.ZAdd(ctx, spec.indexKey, refreshed...).Err(); err != nil {
			logger.LegacyPrintf("repository.concurrency", "Warning: refresh %d active index members in %s failed: %v", len(refreshed), spec.indexKey, err)
		}
	}
	c.removeActiveIndexMembers(ctx, spec.indexKey, staleMembers)
	return nil
}

// CleanupStaleProcessSlots 启动时清理非当前进程前缀的槽位。
// 清理从活跃索引取得候选，包含 score 已过期的成员和崩溃进程留下的槽位。
// 索引上线前遗留的等待计数由一次性迁移清扫处理。
// API Key 槽位仅用于统计：每次写入或读取都会按分数裁剪过期成员，key 自带 TTL，
// 可在一个槽位 TTL 内自愈，因此不参与启动清理。
func (c *concurrencyCache) CleanupStaleProcessSlots(ctx context.Context, activeRequestPrefix string) error {
	if activeRequestPrefix == "" {
		return nil
	}
	if err := c.sweepLegacyWaitKeysOnce(ctx); err != nil {
		return err
	}
	now, err := c.redisUnixSeconds(ctx)
	if err != nil {
		return err
	}

	providerMembers, err := c.allIndexMembers(ctx, providerActiveIndexKey)
	if err != nil {
		return err
	}
	if err := c.cleanupStaleProcessSlotsForIndex(ctx, providerSlotIndex, providerMembers, activeRequestPrefix, now); err != nil {
		return err
	}

	userMembers, err := c.allIndexMembers(ctx, userActiveIndexKey)
	if err != nil {
		return err
	}
	return c.cleanupStaleProcessSlotsForIndex(ctx, userSlotIndex, userMembers, activeRequestPrefix, now)
}

// sweepLegacyWaitKeysOnce 一次性清扫活跃索引机制上线前遗留的等待计数键。
// 等待计数在有流量时会不断刷新 TTL、无法自然过期，而索引不认识旧键，
// 因此这里例外地做一次 SCAN，用 marker 键保证整个 Redis 数据生命周期内只执行一次。
// 先清扫后写 marker：清扫失败时下次启动会重试；并发实例重复清扫是幂等的。
func (c *concurrencyCache) sweepLegacyWaitKeysOnce(ctx context.Context) error {
	exists, err := c.rdb.Exists(ctx, legacyWaitSweepMarkerKey).Result()
	if err != nil {
		return fmt.Errorf("check legacy wait sweep marker: %w", err)
	}
	if exists > 0 {
		return nil
	}
	for _, pattern := range []string{providerWaitKeyPrefix + "*", waitQueueKeyPrefix + "*"} {
		var cursor uint64
		for {
			keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 200).Result()
			if err != nil {
				return fmt.Errorf("scan legacy wait keys %s: %w", pattern, err)
			}
			if len(keys) > 0 {
				if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
					return fmt.Errorf("delete legacy wait keys: %w", err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	if err := c.rdb.Set(ctx, legacyWaitSweepMarkerKey, "1", 0).Err(); err != nil {
		return fmt.Errorf("set legacy wait sweep marker: %w", err)
	}
	return nil
}

// allIndexMembers 返回索引中全部 member（含 score 已过期的）。
// 启动清理必须覆盖过期成员：长时间停机后 score 过期的候选恰恰最可能持有死进程残留。
func (c *concurrencyCache) allIndexMembers(ctx context.Context, indexKey string) ([]string, error) {
	members, err := c.rdb.ZRange(ctx, indexKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("read active index %s: %w", indexKey, err)
	}
	return members, nil
}

// cleanupStaleProcessSlotsForIndex 逐个处理索引中的提供商/用户。
// Lua 脚本一次只碰一个槽位 key，兼容 Redis Cluster，随后删除重启后已失效的等待计数；
// 索引 member 的去留由脚本返回的剩余槽位数决定，最后批量写回。
func (c *concurrencyCache) cleanupStaleProcessSlotsForIndex(
	ctx context.Context,
	spec slotIndexSpec,
	members []string,
	activeRequestPrefix string,
	now int64,
) error {
	staleMembers := make([]string, 0)
	refreshed := make([]redis.Z, 0)
	for _, member := range members {
		id, err := strconv.ParseInt(member, 10, 64)
		if err != nil || id <= 0 {
			staleMembers = append(staleMembers, member)
			continue
		}

		_, remaining, err := runScriptInt64Pair(ctx, c.rdb, startupCleanupSlotScript, []string{spec.slotKey(id)}, activeRequestPrefix, c.slotTTLSeconds)
		if err != nil {
			return fmt.Errorf("cleanup stale process slots %s: %w", spec.slotKey(id), err)
		}
		// 等待计数属于已死进程，直接删除；剩余槽位（当前进程前缀）决定索引 member 去留。
		if err := c.rdb.Del(ctx, spec.waitKey(id)).Err(); err != nil {
			return fmt.Errorf("delete stale wait key %s: %w", spec.waitKey(id), err)
		}
		if remaining > 0 {
			refreshed = append(refreshed, redis.Z{
				Score:  float64(now + int64(c.slotTTLSeconds)),
				Member: member,
			})
		} else {
			staleMembers = append(staleMembers, member)
		}
	}
	if len(refreshed) > 0 {
		if err := c.rdb.ZAdd(ctx, spec.indexKey, refreshed...).Err(); err != nil {
			logger.LegacyPrintf("repository.concurrency", "Warning: refresh %d active index members in %s failed: %v", len(refreshed), spec.indexKey, err)
		}
	}
	c.removeActiveIndexMembers(ctx, spec.indexKey, staleMembers)
	return nil
}
