package rediscache

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache/codec"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newSchedulerCacheUnit(t *testing.T) *schedulerCache {
	cache, _ := newSchedulerCacheUnitWithRedis(t)
	return cache
}

func newSchedulerCacheUnitWithRedis(t *testing.T) (*schedulerCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := newSchedulerCacheWithChunkSizes(rdb, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize)
	return cache, mr
}

func TestSchedulerCacheWriteProviderIDsSkipsUnencodableTimes(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	invalidTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)

	providerIDs, err := cache.writeProviderIDs(ctx, []providercore.Record{
		{ID: 111, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
		{ID: 112, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, ExpiresAt: &invalidTime},
	})
	require.NoError(t, err)
	require.Equal(t, []int64{111}, providerIDs)

	cached, err := cache.GetProvider(ctx, 111)
	require.NoError(t, err)
	require.NotNil(t, cached)

	invalid, err := cache.GetProvider(ctx, 112)
	require.NoError(t, err)
	require.Nil(t, invalid)
}

func TestSchedulerCacheSetProviderClearsUnencodablePayload(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)

	provider := providercore.Record{ID: 113, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}
	require.NoError(t, cache.SetProvider(ctx, &provider))

	invalidTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	provider.ExpiresAt = &invalidTime
	require.NoError(t, cache.SetProvider(ctx, &provider))

	cached, err := cache.GetProvider(ctx, provider.ID)
	require.NoError(t, err)
	require.Nil(t, cached)
}

func TestSchedulerCacheUpdateLastUsedClearsUnencodablePayload(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	provider := providercore.Record{ID: 114, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}
	require.NoError(t, cache.SetProvider(ctx, &provider))

	invalidTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{provider.ID: invalidTime}))

	cached, err := cache.GetProvider(ctx, provider.ID)
	require.NoError(t, err)
	require.Nil(t, cached)
}

func TestSchedulerCacheSnapshotProviderIDReusePreservesPayloadAndMembers(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheUnitWithRedis(t)
	invalidTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	validOne := providercore.Record{
		ID:          701,
		Name:        "first",
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"z": "last", "a": "first"}},
		Extra:       map[string]any{"openai_passthrough": true},
		GroupIDs:    []int64{17},
	}
	validTwo := providercore.Record{ID: 702, Name: "second", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}
	invalid := providercore.Record{ID: 799, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, ExpiresAt: &invalidTime}
	providers := []providercore.Record{validOne, invalid, validTwo, validOne}

	single := scheduler.SchedulerBucket{GroupID: 17, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeSingle}
	singleToken, err := cache.CaptureBucketWriteToken(ctx, single)
	require.NoError(t, err)
	providerIDs, err := cache.SetSnapshotAndReturnProviderIDs(ctx, single, singleToken, providers)
	require.NoError(t, err)
	require.Equal(t, []int64{701, 702, 701}, providerIDs, "应保留可编码提供商的原顺序和重复项")

	wantFull := historicalSchedulerPayload(t, "wantFull")
	wantMeta := historicalSchedulerPayload(t, "wantMeta")
	fullBefore, err := cache.rdb.Get(ctx, schedulerProviderKey("701")).Bytes()
	require.NoError(t, err)
	metaBefore, err := cache.rdb.Get(ctx, schedulerProviderMetaKey("701")).Bytes()
	require.NoError(t, err)
	require.Equal(t, wantFull, fullBefore)
	require.Equal(t, wantMeta, metaBefore)

	forced := scheduler.SchedulerBucket{GroupID: 17, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeForced}
	forcedToken, err := cache.CaptureBucketWriteToken(ctx, forced)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshotByProviderIDs(ctx, forced, forcedToken, providerIDs))

	fullAfter, err := cache.rdb.Get(ctx, schedulerProviderKey("701")).Bytes()
	require.NoError(t, err)
	metaAfter, err := cache.rdb.Get(ctx, schedulerProviderMetaKey("701")).Bytes()
	require.NoError(t, err)
	require.Equal(t, fullBefore, fullAfter, "ID-only 路径不得重写完整提供商键")
	require.Equal(t, metaBefore, metaAfter, "ID-only 路径不得重写调度元数据键")

	for _, bucket := range []scheduler.SchedulerBucket{single, forced} {
		version, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerActivePrefix, bucket)).Result()
		require.NoError(t, err)
		members, err := cache.rdb.ZRange(ctx, schedulerSnapshotKey(bucket, version), 0, -1).Result()
		require.NoError(t, err)
		require.Equal(t, []string{"702", "701"}, members, bucket.String())
	}
	missing, err := cache.GetProvider(ctx, invalid.ID)
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestSchedulerCacheSetSnapshotMatchesIDPublishing(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheUnitWithRedis(t)
	invalidTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	validOne := providercore.Record{
		ID:          721,
		Name:        "first",
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"source": "target"}},
		Extra:       map[string]any{"openai_passthrough": true},
		GroupIDs:    []int64{21},
	}
	validTwo := providercore.Record{ID: 722, Name: "second", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}
	invalid := providercore.Record{ID: 799, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, ExpiresAt: &invalidTime}
	providers := []providercore.Record{validOne, invalid, validTwo, validOne}

	normal := scheduler.SchedulerBucket{GroupID: 21, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeSingle}
	normalToken, err := cache.CaptureBucketWriteToken(ctx, normal)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, normal, normalToken, providers))

	fullBefore, err := cache.rdb.Get(ctx, schedulerProviderKey("721")).Bytes()
	require.NoError(t, err)
	metaBefore, err := cache.rdb.Get(ctx, schedulerProviderMetaKey("721")).Bytes()
	require.NoError(t, err)

	idOnly := scheduler.SchedulerBucket{GroupID: 21, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeForced}
	idOnlyToken, err := cache.CaptureBucketWriteToken(ctx, idOnly)
	require.NoError(t, err)
	providerIDs, err := cache.SetSnapshotAndReturnProviderIDs(ctx, idOnly, idOnlyToken, providers)
	require.NoError(t, err)
	require.Equal(t, []int64{721, 722, 721}, providerIDs)

	fullAfter, err := cache.rdb.Get(ctx, schedulerProviderKey("721")).Bytes()
	require.NoError(t, err)
	metaAfter, err := cache.rdb.Get(ctx, schedulerProviderMetaKey("721")).Bytes()
	require.NoError(t, err)
	require.Equal(t, fullBefore, fullAfter, "普通快照和 ID 发布必须写入相同完整提供商 payload")
	require.Equal(t, metaBefore, metaAfter, "普通快照和 ID 发布必须写入相同元数据 payload")

	for _, bucket := range []scheduler.SchedulerBucket{normal, idOnly} {
		version, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerActivePrefix, bucket)).Result()
		require.NoError(t, err)
		members, err := cache.rdb.ZRange(ctx, schedulerSnapshotKey(bucket, version), 0, -1).Result()
		require.NoError(t, err)
		require.Equal(t, []string{"722", "721"}, members, bucket.String())
	}
}

func TestSchedulerCacheSnapshotProviderIDReuseKeepsEmptySnapshotSemantics(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	invalidTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	providers := []providercore.Record{{ID: 811, Platform: capability.PlatformOpenAI, ExpiresAt: &invalidTime}}

	single := scheduler.SchedulerBucket{GroupID: 18, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeSingle}
	singleToken, err := cache.CaptureBucketWriteToken(ctx, single)
	require.NoError(t, err)
	providerIDs, err := cache.SetSnapshotAndReturnProviderIDs(ctx, single, singleToken, providers)
	require.NoError(t, err)
	require.Empty(t, providerIDs)

	forced := scheduler.SchedulerBucket{GroupID: 18, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeForced}
	forcedToken, err := cache.CaptureBucketWriteToken(ctx, forced)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshotByProviderIDs(ctx, forced, forcedToken, providerIDs))

	for _, bucket := range []scheduler.SchedulerBucket{single, forced} {
		ready, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerReadyPrefix, bucket)).Result()
		require.NoError(t, err)
		require.Equal(t, "1", ready)
		snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
		require.NoError(t, err)
		require.False(t, hit, bucket.String())
		require.Nil(t, snapshot)
	}
}

func TestSchedulerCacheSetSnapshotByProviderIDsKeepsFencing(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	bucket := scheduler.SchedulerBucket{GroupID: 19, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeForced}

	err := cache.SetSnapshotByProviderIDs(ctx, bucket, scheduler.SchedulerBucketWriteToken{}, []int64{901})
	require.ErrorIs(t, err, scheduler.ErrSchedulerBucketWriteFenced)
	_, err = cache.rdb.Get(ctx, schedulerBucketKey(schedulerVersionPrefix, bucket)).Result()
	require.ErrorIs(t, err, redis.Nil)

	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.RetireBucket(ctx, bucket))
	err = cache.SetSnapshotByProviderIDs(ctx, bucket, token, []int64{901})
	require.ErrorIs(t, err, scheduler.ErrSchedulerBucketRetired)
}

func TestSchedulerCacheSetSnapshotByProviderIDsDoesNotResurrectDeletedProvider(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	provider := providercore.Record{ID: 902, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}
	single := scheduler.SchedulerBucket{GroupID: 20, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeSingle}
	singleToken, err := cache.CaptureBucketWriteToken(ctx, single)
	require.NoError(t, err)
	providerIDs, err := cache.SetSnapshotAndReturnProviderIDs(ctx, single, singleToken, []providercore.Record{provider})
	require.NoError(t, err)
	require.Equal(t, []int64{provider.ID}, providerIDs)
	require.NoError(t, cache.DeleteProvider(ctx, provider.ID))

	forced := scheduler.SchedulerBucket{GroupID: 20, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeForced}
	forcedToken, err := cache.CaptureBucketWriteToken(ctx, forced)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshotByProviderIDs(ctx, forced, forcedToken, providerIDs))

	full, err := cache.GetProvider(ctx, provider.ID)
	require.NoError(t, err)
	require.Nil(t, full, "ID-only 发布不得复活已删除的完整提供商键")
	snapshot, hit, err := cache.GetSnapshot(ctx, forced)
	require.NoError(t, err)
	require.False(t, hit, "元数据缺失时必须安全回源，而不是返回残缺快照")
	require.Nil(t, snapshot)
}

func TestMarshalSchedulerCacheProviderKeepsEncodingJSONWireFormat(t *testing.T) {
	cases := []struct {
		name     string
		provider providercore.Record
	}{
		{name: "nil collections", provider: providercore.Record{ID: 801}},
		{name: "empty collections", provider: providercore.Record{
			ID:          802,
			Credentials: map[string]any{},
			Extra:       map[string]any{},
			GroupIDs:    []int64{},
			Groups:      []*accessview.GroupConfig{},
		}},
		{name: "nested maps and escaping", provider: providercore.Record{
			ID:          803,
			Credentials: map[string]any{"model_mapping": map[string]any{"z": "<last>", "a": "&first"}},
			Extra:       map[string]any{"openai_passthrough": true},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full, meta, err := marshalSchedulerCacheProvider(tc.provider)
			require.NoError(t, err)
			wantFull := historicalSchedulerPayload(t, "wantFull")
			wantMeta := historicalSchedulerPayload(t, "wantMeta")
			require.Equal(t, wantFull, full)
			require.Equal(t, wantMeta, meta)
		})
	}
}

func TestBuildSchedulerMetadataProvider_KeepsOpenAIAPIKeyProtocolFields(t *testing.T) {
	provider := providercore.Record{
		ID:       42,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"openai_workload_capabilities": []any{"text_generation"},
			"access_token":                 "drop-me",
		},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    providercore.OpenAIWSIngressModePassthrough,
			"openai_ws_force_http":                          true,
			"openai_text_route_mode":                        "force_chat_completions",
			"openai_responses_probe_status":                 "unsupported",
			"openai_compact_mode":                           "force_off",
			"openai_native_compaction_v2_mode":              "force_on",
			"openai_responses_continuation_supported":       true,
			"mixed_scheduling":                              true,
			"unused_large_field":                            "drop-me",
		},
	}

	got := buildSchedulerMetadataProvider(provider)

	require.Equal(t, true, got.Extra["openai_apikey_responses_websockets_v2_enabled"])
	require.Equal(t, providercore.OpenAIWSIngressModePassthrough, got.Extra["openai_apikey_responses_websockets_v2_mode"])
	require.Equal(t, true, got.Extra["openai_ws_force_http"])
	require.Equal(t, "force_chat_completions", got.Extra["openai_text_route_mode"])
	require.NotContains(t, got.Extra, "openai_responses_probe_status")
	require.Equal(t, "force_off", got.Extra["openai_compact_mode"])
	require.Equal(t, "force_on", got.Extra["openai_native_compaction_v2_mode"])
	require.Equal(t, true, got.Extra["openai_responses_continuation_supported"])
	require.NotContains(t, got.Extra, "mixed_scheduling")
	require.Nil(t, got.Extra["unused_large_field"])
	require.Equal(t, []any{"text_generation"}, got.Credentials["openai_workload_capabilities"])
	require.Nil(t, got.Credentials["access_token"])
}

func TestBuildSchedulerMetadataProvider_KeepsGrokMediaEligibility(t *testing.T) {
	t.Run("explicit override", func(t *testing.T) {
		provider := providercore.Record{
			ID:       43,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Extra: map[string]any{
				providercore.GrokMediaEligibleExtraKey: false,
				"unused_large_field":                   "drop-me",
			},
		}

		got := buildSchedulerMetadataProvider(provider)

		eligible, reason := providercore.GrokMediaGenerationEligibility(&got, provideradapter.GrokTierRules())
		require.False(t, eligible)
		require.Equal(t, "override_disabled", reason)
		require.Equal(t, false, got.Extra[providercore.GrokMediaEligibleExtraKey])
		require.Nil(t, got.Extra["unused_large_field"])
	})

	t.Run("forbidden billing observation", func(t *testing.T) {
		provider := providercore.Record{
			ID:       44,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Extra: map[string]any{
				"grok_billing_snapshot": map[string]any{
					"status_code":         200,
					"weekly_status_code":  403,
					"monthly_status_code": 200,
				},
			},
		}

		got := buildSchedulerMetadataProvider(provider)

		eligible, reason := providercore.GrokMediaGenerationEligibility(&got, provideradapter.GrokTierRules())
		require.False(t, eligible)
		require.Equal(t, "billing_forbidden", reason)
		require.NotNil(t, got.Extra["grok_billing_snapshot"])
	})
}

func TestBuildSchedulerMetadataProvider_KeepsSlimGroupMembership(t *testing.T) {
	provider := providercore.Record{
		ID:       42,
		Platform: capability.PlatformAnthropic,
		GroupIDs: []int64{7, 9, 7, 0},
		ProviderGroups: []providercore.GroupMembership{
			{
				ProviderID: 42,
				GroupID:    7,
				Provider:   &providercore.Record{ID: 42, Name: "drop-from-metadata"},
				Group:      &accessview.GroupConfig{ID: 7, Name: "drop-from-metadata"},
			},
			{
				ProviderID: 42,
				GroupID:    11,
				Group:      &accessview.GroupConfig{ID: 11, Name: "drop-from-metadata"},
			},
			{
				ProviderID: 42,
				GroupID:    0,
			},
		},
	}

	got := buildSchedulerMetadataProvider(provider)

	require.Equal(t, []int64{7, 9, 11}, got.GroupIDs)
	require.Len(t, got.ProviderGroups, 2)
	require.Equal(t, int64(42), got.ProviderGroups[0].ProviderID)
	require.Equal(t, int64(7), got.ProviderGroups[0].GroupID)
	require.Nil(t, got.ProviderGroups[0].Provider)
	require.Nil(t, got.ProviderGroups[0].Group)
	require.Equal(t, int64(11), got.ProviderGroups[1].GroupID)
	require.Nil(t, got.Groups)
}

func TestBuildSchedulerMetadataProvider_KeepsQuotaAutoPauseFields(t *testing.T) {
	provider := providercore.Record{
		ID: 88,
		Extra: map[string]any{
			"codex_5h_used_percent":        12.34,
			"codex_7d_used_percent":        56.78,
			"codex_5h_reset_at":            "2026-05-29T10:00:00Z",
			"codex_7d_reset_at":            "2026-06-01T10:00:00Z",
			"codex_5h_reset_after_seconds": 300,
			"codex_7d_reset_after_seconds": 600,
			"codex_usage_updated_at":       "2026-05-29T09:00:00Z",
			"auto_pause_5h_threshold":      0.95,
			"auto_pause_7d_threshold":      0.96,
			"auto_pause_5h_disabled":       true,
			"auto_pause_7d_disabled":       false,
		},
	}

	got := buildSchedulerMetadataProvider(provider)

	require.Equal(t, 12.34, got.Extra["codex_5h_used_percent"])
	require.Equal(t, 56.78, got.Extra["codex_7d_used_percent"])
	require.Equal(t, "2026-05-29T10:00:00Z", got.Extra["codex_5h_reset_at"])
	require.Equal(t, "2026-06-01T10:00:00Z", got.Extra["codex_7d_reset_at"])
	require.Equal(t, 300, got.Extra["codex_5h_reset_after_seconds"])
	require.Equal(t, 600, got.Extra["codex_7d_reset_after_seconds"])
	require.Equal(t, "2026-05-29T09:00:00Z", got.Extra["codex_usage_updated_at"])
	require.Equal(t, 0.95, got.Extra["auto_pause_5h_threshold"])
	require.Equal(t, 0.96, got.Extra["auto_pause_7d_threshold"])
	require.Equal(t, true, got.Extra["auto_pause_5h_disabled"])
	require.Equal(t, false, got.Extra["auto_pause_7d_disabled"])
}

func TestBuildSchedulerMetadataProvider_KeepsQuotaStateForCachedProviders(t *testing.T) {
	now := time.Now().UTC()
	activeStart := now.Add(-time.Hour).Format(time.RFC3339)
	expiredDailyStart := now.Add(-25 * time.Hour).Format(time.RFC3339)
	expiredWeeklyStart := now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	weeklyResetDay := float64(now.AddDate(0, 0, 1).Weekday())

	cases := []struct {
		name          string
		platform      string
		typ           string
		extra         map[string]any
		quotaExceeded bool
	}{
		{
			name: "anthropic api key total quota exhausted", platform: capability.PlatformAnthropic, typ: capability.ProviderTypeAPIKey,
			extra: map[string]any{"quota_limit": 10.0, "quota_used": 10.0}, quotaExceeded: true,
		},
		{
			name: "gemini api key rolling daily quota exhausted", platform: capability.PlatformGemini, typ: capability.ProviderTypeAPIKey,
			extra: map[string]any{
				"quota_daily_limit": 20.0, "quota_daily_used": 20.0,
				"quota_daily_start": activeStart, "quota_daily_reset_mode": "rolling",
			}, quotaExceeded: true,
		},
		{
			name: "gemini api key expired rolling daily window", platform: capability.PlatformGemini, typ: capability.ProviderTypeAPIKey,
			extra: map[string]any{
				"quota_daily_limit": 20.0, "quota_daily_used": 20.0,
				"quota_daily_start": expiredDailyStart, "quota_daily_reset_mode": "rolling",
			},
		},
		{
			name: "bedrock fixed weekly quota exhausted", platform: capability.PlatformAnthropic, typ: capability.ProviderTypeBedrock,
			extra: map[string]any{
				"quota_weekly_limit": 30.0, "quota_weekly_used": 30.0, "quota_weekly_start": activeStart,
				"quota_weekly_reset_mode": "fixed", "quota_weekly_reset_day": weeklyResetDay,
				"quota_weekly_reset_hour": 0.0, "quota_reset_timezone": "UTC",
			}, quotaExceeded: true,
		},
		{
			name: "bedrock expired fixed weekly window", platform: capability.PlatformAnthropic, typ: capability.ProviderTypeBedrock,
			extra: map[string]any{
				"quota_weekly_limit": 30.0, "quota_weekly_used": 30.0, "quota_weekly_start": expiredWeeklyStart,
				"quota_weekly_reset_mode": "fixed", "quota_weekly_reset_day": weeklyResetDay,
				"quota_weekly_reset_hour": 0.0, "quota_reset_timezone": "UTC",
			},
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extra := make(map[string]any, len(tc.extra)+1)
			for key, value := range tc.extra {
				extra[key] = value
			}
			extra["unrelated"] = "drop me"
			provider := providercore.Record{
				ID: int64(46690 + i), Platform: tc.platform, Type: tc.typ, Extra: extra,
				Status: billing.StatusActive, Schedulable: true,
			}
			cache := newSchedulerCacheUnit(t)
			ctx := context.Background()
			bucket := scheduler.SchedulerBucket{GroupID: int64(46690 + i), Platform: tc.platform, Mode: scheduler.SchedulerModeSingle}
			token, err := cache.CaptureBucketWriteToken(ctx, bucket)
			require.NoError(t, err)
			require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []providercore.Record{provider}))

			snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
			require.NoError(t, err)
			require.True(t, hit)
			require.Len(t, snapshot, 1)
			cached := snapshot[0]
			require.Equal(t, tc.extra, cached.Extra)
			require.NotContains(t, cached.Extra, "unrelated")
			require.Equal(t, tc.quotaExceeded, cached.IsQuotaExceeded())
			require.Equal(t, !tc.quotaExceeded, cached.IsSchedulable())
		})
	}
}

func TestBuildSchedulerMetadataProvider_KeepsModelRateLimits(t *testing.T) {
	provider := providercore.Record{
		ID:       90,
		Platform: capability.PlatformAntigravity,
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"gemini-3-flash": map[string]any{
					"rate_limit_reset_at": "2026-05-30T10:10:00Z",
				},
				"antigravity:gemini": map[string]any{
					"rate_limit_reset_at": "2026-05-30T10:10:00Z",
				},
			},
			"unused_large_field": "drop-me",
		},
	}

	got := buildSchedulerMetadataProvider(provider)

	limits, ok := got.Extra["model_rate_limits"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, limits, "gemini-3-flash")
	require.Contains(t, limits, "antigravity:gemini")
	require.Nil(t, got.Extra["unused_large_field"])
}

func TestBuildSchedulerMetadataProvider_KeepsSparkShadowRoutingIdentity(t *testing.T) {
	parentID := int64(100)
	provider := providercore.Record{
		ID:               200,
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   providercore.QuotaDimensionSpark,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
			},
			"compact_model_mapping": map[string]any{
				"gpt-5.4": "gpt-5.4-openai-compact",
			},
			"access_token": "drop-me",
		},
	}

	got := buildSchedulerMetadataProvider(provider)

	require.NotNil(t, got.ParentProviderID)
	require.Equal(t, parentID, *got.ParentProviderID)
	require.Equal(t, providercore.QuotaDimensionSpark, got.QuotaDimension)
	require.Equal(t, map[string]any{"gpt-5.3-codex-spark": "gpt-5.3-codex-spark"}, got.Credentials["model_mapping"])
	require.Equal(t, map[string]any{"gpt-5.4": "gpt-5.4-openai-compact"}, got.Credentials["compact_model_mapping"])
	require.Nil(t, got.Credentials["access_token"])
}

func TestSchedulerCacheBucketRetirementFencesWritersAndReopen(t *testing.T) {
	ctx := context.Background()
	cache, mr := newSchedulerCacheUnitWithRedis(t)
	bucket := scheduler.SchedulerBucket{GroupID: 41, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeSingle}
	otherBucket := scheduler.SchedulerBucket{GroupID: 42, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeSingle}
	provider := providercore.Record{ID: 4101, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}

	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.True(t, token.ValidFor(bucket))
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []providercore.Record{provider}))

	// token 同时绑定完整桶标识和 epoch。
	err = cache.SetSnapshot(ctx, otherBucket, token, []providercore.Record{provider})
	require.ErrorIs(t, err, scheduler.ErrSchedulerBucketWriteFenced)
	_, err = cache.rdb.Get(ctx, schedulerBucketKey(schedulerVersionPrefix, otherBucket)).Result()
	require.ErrorIs(t, err, redis.Nil)
	otherProvider := providercore.Record{ID: 4201, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}
	otherToken, err := cache.CaptureBucketWriteToken(ctx, otherBucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, otherBucket, otherToken, []providercore.Record{otherProvider}))
	otherEpoch := otherToken.Epoch

	activeVersion, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerActivePrefix, bucket)).Result()
	require.NoError(t, err)
	require.NoError(t, cache.RetireBucket(ctx, bucket))
	retiredEpoch, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerEpochPrefix, bucket)).Int64()
	require.NoError(t, err)
	require.Greater(t, retiredEpoch, token.Epoch)

	// 重复退休保持当前 epoch。
	require.NoError(t, cache.RetireBucket(ctx, bucket))
	retiredEpochAgain, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerEpochPrefix, bucket)).Int64()
	require.NoError(t, err)
	require.Equal(t, retiredEpoch, retiredEpochAgain)

	// ready/active 被原子删除后，新读请求会未命中；退休前已取得 activeVersion 的读请求仍可完成。
	_, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.False(t, hit)
	ids, err := cache.rdb.ZRange(ctx, schedulerSnapshotKey(bucket, activeVersion), 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"4101"}, ids)
	ttl, err := cache.rdb.TTL(ctx, schedulerSnapshotKey(bucket, activeVersion)).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, time.Duration(snapshotGraceTTLSeconds)*time.Second)

	buckets, err := cache.ListBuckets(ctx)
	require.NoError(t, err)
	require.NotContains(t, buckets, bucket)
	require.Contains(t, buckets, otherBucket)
	otherSnapshot, otherHit, err := cache.GetSnapshot(ctx, otherBucket)
	require.NoError(t, err)
	require.True(t, otherHit)
	require.Len(t, otherSnapshot, 1)
	require.Equal(t, otherProvider.ID, otherSnapshot[0].ID)
	otherEpochAfter, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerEpochPrefix, otherBucket)).Int64()
	require.NoError(t, err)
	require.Equal(t, otherEpoch, otherEpochAfter)

	_, err = cache.CaptureBucketWriteToken(ctx, bucket)
	require.ErrorIs(t, err, scheduler.ErrSchedulerBucketRetired)
	versionBeforeRejectedWrite, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerVersionPrefix, bucket)).Int64()
	require.NoError(t, err)
	err = cache.SetSnapshot(ctx, bucket, token, []providercore.Record{provider})
	require.ErrorIs(t, err, scheduler.ErrSchedulerBucketRetired)
	versionAfterRejectedWrite, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerVersionPrefix, bucket)).Int64()
	require.NoError(t, err)
	require.Equal(t, versionBeforeRejectedWrite, versionAfterRejectedWrite, "fenced writers must not allocate a new version")
	retired, err := cache.rdb.Exists(ctx, schedulerBucketKey(schedulerRetiredPrefix, bucket)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, retired, "ordinary writers must never clear the tombstone")
	mr.FastForward(time.Duration(snapshotGraceTTLSeconds+1) * time.Second)
	exists, err := cache.rdb.Exists(ctx, schedulerSnapshotKey(bucket, activeVersion)).Result()
	require.NoError(t, err)
	require.Zero(t, exists, "retired active snapshot must expire after the in-flight grace period")

	newToken, err := cache.ReopenBucket(ctx, bucket)
	require.NoError(t, err)
	require.True(t, newToken.ValidFor(bucket))
	require.Equal(t, retiredEpoch, newToken.Epoch)
	reopenedAgain, err := cache.ReopenBucket(ctx, bucket)
	require.NoError(t, err)
	require.Equal(t, newToken, reopenedAgain, "reopen must be idempotent within one retirement generation")
	err = cache.SetSnapshot(ctx, bucket, token, []providercore.Record{provider})
	require.ErrorIs(t, err, scheduler.ErrSchedulerBucketWriteFenced)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, newToken, []providercore.Record{provider}))
	reopenedWhileOpen, err := cache.ReopenBucket(ctx, bucket)
	require.NoError(t, err)
	require.Equal(t, newToken, reopenedWhileOpen)

	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)
	require.Equal(t, provider.ID, snapshot[0].ID)
}

func TestSchedulerCacheActivationIsFencedAfterRetire(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	bucket := scheduler.SchedulerBucket{GroupID: 51, Platform: capability.PlatformAnthropic, Mode: scheduler.SchedulerModeMixed}
	provider := providercore.Record{ID: 5101, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}

	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	version, err := cache.allocateSnapshotVersion(ctx, bucket, token)
	require.NoError(t, err)
	_, err = cache.writeSnapshotVersionAndReturnProviderIDs(ctx, bucket, version, []providercore.Record{provider})
	require.NoError(t, err)

	// 确定性竞态 C：退休与权威重开都发生在 INCR/写入之后、旧 writer 激活之前。
	require.NoError(t, cache.RetireBucket(ctx, bucket))
	_, err = cache.ReopenBucket(ctx, bucket)
	require.NoError(t, err)
	err = cache.activateSnapshotVersion(ctx, bucket, token, version)
	require.ErrorIs(t, err, scheduler.ErrSchedulerBucketWriteFenced)

	exists, err := cache.rdb.Exists(ctx, schedulerSnapshotKey(bucket, version)).Result()
	require.NoError(t, err)
	require.Zero(t, exists, "fenced activation must delete its unpublished snapshot")
	exists, err = cache.rdb.Exists(
		ctx,
		schedulerBucketKey(schedulerReadyPrefix, bucket),
		schedulerBucketKey(schedulerActivePrefix, bucket),
	).Result()
	require.NoError(t, err)
	require.Zero(t, exists)
	buckets, err := cache.ListBuckets(ctx)
	require.NoError(t, err)
	require.NotContains(t, buckets, bucket)
}

func TestSchedulerCacheConcurrentReopenReturnsSameToken(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	bucket := scheduler.SchedulerBucket{GroupID: 53, Platform: capability.PlatformOpenAI, Mode: scheduler.SchedulerModeForced}
	provider := providercore.Record{ID: 5301, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}

	oldToken, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.RetireBucket(ctx, bucket))

	type reopenResult struct {
		token scheduler.SchedulerBucketWriteToken
		err   error
	}
	start := make(chan struct{})
	results := make(chan reopenResult, 2)
	for range 2 {
		go func() {
			<-start
			token, err := cache.ReopenBucket(ctx, bucket)
			results <- reopenResult{token: token, err: err}
		}()
	}
	close(start)
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.token, second.token)
	require.Greater(t, first.token.Epoch, oldToken.Epoch)

	require.ErrorIs(t, cache.SetSnapshot(ctx, bucket, oldToken, []providercore.Record{provider}), scheduler.ErrSchedulerBucketWriteFenced)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, first.token, []providercore.Record{provider}))
}

func TestSchedulerCacheReopenExpiresPreviousActiveSnapshot(t *testing.T) {
	ctx := context.Background()
	cache, mr := newSchedulerCacheUnitWithRedis(t)
	bucket := scheduler.SchedulerBucket{GroupID: 52, Platform: capability.PlatformGemini, Mode: scheduler.SchedulerModeForced}
	provider := providercore.Record{ID: 5201, Platform: capability.PlatformGemini, Type: capability.ProviderTypeAPIKey}

	oldToken, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, oldToken, []providercore.Record{provider}))
	oldVersion, err := cache.rdb.Get(ctx, schedulerBucketKey(schedulerActivePrefix, bucket)).Result()
	require.NoError(t, err)
	retiredEpoch := oldToken.Epoch + 1
	require.NoError(t, cache.rdb.Set(ctx, schedulerBucketKey(schedulerEpochPrefix, bucket), retiredEpoch, 0).Err())
	require.NoError(t, cache.rdb.Set(ctx, schedulerBucketKey(schedulerRetiredPrefix, bucket), retiredEpoch, 0).Err())

	newToken, err := cache.ReopenBucket(ctx, bucket)
	require.NoError(t, err)
	require.Equal(t, retiredEpoch, newToken.Epoch)
	_, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.False(t, hit)
	ttl, err := cache.rdb.TTL(ctx, schedulerSnapshotKey(bucket, oldVersion)).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, time.Duration(snapshotGraceTTLSeconds)*time.Second)

	require.ErrorIs(t, cache.SetSnapshot(ctx, bucket, oldToken, []providercore.Record{provider}), scheduler.ErrSchedulerBucketWriteFenced)
	mr.FastForward(time.Duration(snapshotGraceTTLSeconds+1) * time.Second)
	exists, err := cache.rdb.Exists(ctx, schedulerSnapshotKey(bucket, oldVersion)).Result()
	require.NoError(t, err)
	require.Zero(t, exists)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, newToken, []providercore.Record{provider}))
}

func TestSchedulerCacheGroupLifecycleLeaseConcurrentAcquireSingleOwner(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	const groupID int64 = 71

	type result struct {
		lease    scheduler.SchedulerGroupLifecycleLease
		acquired bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 32)
	for range 32 {
		go func() {
			<-start
			lease, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, groupID, time.Minute)
			results <- result{lease: lease, acquired: acquired, err: err}
		}()
	}
	close(start)

	var owner scheduler.SchedulerGroupLifecycleLease
	acquiredCount := 0
	for range 32 {
		got := <-results
		require.NoError(t, got.err)
		if got.acquired {
			acquiredCount++
			owner = got.lease
			require.True(t, got.lease.ValidFor(groupID))
		} else {
			require.Equal(t, scheduler.SchedulerGroupLifecycleLease{}, got.lease)
		}
	}
	require.Equal(t, 1, acquiredCount)
	require.Len(t, owner.OwnerToken, schedulerGroupLifecycleOwnerTokenBytes*2)
	require.Equal(t, strings.ToLower(owner.OwnerToken), owner.OwnerToken)
	decodedOwner, err := hex.DecodeString(owner.OwnerToken)
	require.NoError(t, err)
	require.Len(t, decodedOwner, schedulerGroupLifecycleOwnerTokenBytes)

	require.NoError(t, cache.ReleaseGroupLifecycleLease(ctx, owner))
	next, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, groupID, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	require.True(t, next.ValidFor(groupID))
	require.NotEqual(t, owner.OwnerToken, next.OwnerToken)
	require.NoError(t, cache.ReleaseGroupLifecycleLease(ctx, next))
}

func TestSchedulerCacheGroupLifecycleLeaseStaleReleaseCannotDeleteSuccessor(t *testing.T) {
	ctx := context.Background()
	cache, mr := newSchedulerCacheUnitWithRedis(t)
	const groupID int64 = 72
	const ttl = time.Minute

	first, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, groupID, ttl)
	require.NoError(t, err)
	require.True(t, acquired)

	mr.FastForward(ttl + time.Second)
	second, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, groupID, ttl)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotEqual(t, first.OwnerToken, second.OwnerToken)

	require.ErrorIs(t, cache.ReleaseGroupLifecycleLease(ctx, first), scheduler.ErrSchedulerGroupLifecycleLeaseLost)
	owner, err := cache.rdb.Get(ctx, schedulerGroupLifecycleLockKey(groupID)).Result()
	require.NoError(t, err)
	require.Equal(t, second.OwnerToken, owner)

	_, acquired, err = cache.TryAcquireGroupLifecycleLease(ctx, groupID, ttl)
	require.NoError(t, err)
	require.False(t, acquired)
	require.NoError(t, cache.ReleaseGroupLifecycleLease(ctx, second))
	require.ErrorIs(t, cache.ReleaseGroupLifecycleLease(ctx, second), scheduler.ErrSchedulerGroupLifecycleLeaseLost)
}

func TestSchedulerCacheGroupLifecycleLeaseExpiredReleaseIsLost(t *testing.T) {
	ctx := context.Background()
	cache, mr := newSchedulerCacheUnitWithRedis(t)
	const groupID int64 = 73
	const ttl = time.Minute

	lease, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, groupID, ttl)
	require.NoError(t, err)
	require.True(t, acquired)
	mr.FastForward(ttl + time.Second)

	require.ErrorIs(t, cache.ReleaseGroupLifecycleLease(ctx, lease), scheduler.ErrSchedulerGroupLifecycleLeaseLost)
}

func TestSchedulerCacheGroupLifecycleLeaseWrongOwnerAndCrossGroupAreLost(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	const firstGroupID int64 = 74
	const secondGroupID int64 = 75

	first, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, firstGroupID, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	second, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, secondGroupID, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired, "different groups must acquire independently")
	require.NotEqual(t, first.OwnerToken, second.OwnerToken)

	wrongOwner := first
	wrongOwner.OwnerToken = strings.Repeat("0", schedulerGroupLifecycleOwnerTokenBytes*2)
	if wrongOwner.OwnerToken == first.OwnerToken {
		wrongOwner.OwnerToken = strings.Repeat("1", schedulerGroupLifecycleOwnerTokenBytes*2)
	}
	require.ErrorIs(t, cache.ReleaseGroupLifecycleLease(ctx, wrongOwner), scheduler.ErrSchedulerGroupLifecycleLeaseLost)

	crossGroup := first
	crossGroup.GroupID = secondGroupID
	require.ErrorIs(t, cache.ReleaseGroupLifecycleLease(ctx, crossGroup), scheduler.ErrSchedulerGroupLifecycleLeaseLost)

	firstOwner, err := cache.rdb.Get(ctx, schedulerGroupLifecycleLockKey(firstGroupID)).Result()
	require.NoError(t, err)
	require.Equal(t, first.OwnerToken, firstOwner)
	secondOwner, err := cache.rdb.Get(ctx, schedulerGroupLifecycleLockKey(secondGroupID)).Result()
	require.NoError(t, err)
	require.Equal(t, second.OwnerToken, secondOwner)

	require.NoError(t, cache.ReleaseGroupLifecycleLease(ctx, first))
	require.NoError(t, cache.ReleaseGroupLifecycleLease(ctx, second))
}

func TestSchedulerCacheGroupLifecycleLeaseCanceledContextFailsClosed(t *testing.T) {
	cache := newSchedulerCacheUnit(t)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	lease, acquired, err := cache.TryAcquireGroupLifecycleLease(canceledCtx, 76, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, acquired)
	require.Equal(t, scheduler.SchedulerGroupLifecycleLease{}, lease)

	ctx := context.Background()
	lease, acquired, err = cache.TryAcquireGroupLifecycleLease(ctx, 76, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	require.ErrorIs(t, cache.ReleaseGroupLifecycleLease(canceledCtx, lease), context.Canceled)
	owner, err := cache.rdb.Get(ctx, schedulerGroupLifecycleLockKey(lease.GroupID)).Result()
	require.NoError(t, err)
	require.Equal(t, lease.OwnerToken, owner)
	require.NoError(t, cache.ReleaseGroupLifecycleLease(ctx, lease))
}

func TestSchedulerCacheGroupLifecycleLeaseRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)

	lease, acquired, err := cache.TryAcquireGroupLifecycleLease(ctx, 0, time.Minute)
	require.ErrorIs(t, err, scheduler.ErrSchedulerGroupLifecycleLeaseInvalid)
	require.False(t, acquired)
	require.Equal(t, scheduler.SchedulerGroupLifecycleLease{}, lease)

	lease, acquired, err = cache.TryAcquireGroupLifecycleLease(ctx, 73, 0)
	require.ErrorIs(t, err, scheduler.ErrSchedulerGroupLifecycleLeaseInvalid)
	require.False(t, acquired)
	require.Equal(t, scheduler.SchedulerGroupLifecycleLease{}, lease)

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, cache.ReleaseGroupLifecycleLease(canceledCtx, scheduler.SchedulerGroupLifecycleLease{}), scheduler.ErrSchedulerGroupLifecycleLeaseInvalid)
	keys, err := cache.rdb.DBSize(ctx).Result()
	require.NoError(t, err)
	require.Zero(t, keys)
}

var schedulerCachePayloadBenchmarkSink int

func BenchmarkSchedulerCacheProviderPayloadReuse(b *testing.B) {
	for _, size := range []int{1, 100, 10_000} {
		providers := schedulerCacheBenchmarkProviders(size)
		b.Run(fmt.Sprintf("pair_baseline_%d_providers", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				first, err := benchmarkSchedulerLegacySnapshotPayload(providers)
				if err != nil {
					b.Fatal(err)
				}
				second, err := benchmarkSchedulerLegacySnapshotPayload(providers)
				if err != nil {
					b.Fatal(err)
				}
				schedulerCachePayloadBenchmarkSink = first + second
			}
		})
		b.Run(fmt.Sprintf("pair_reuse_%d_providers", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ids, total, err := benchmarkSchedulerReusableSnapshotPayload(providers)
				if err != nil {
					b.Fatal(err)
				}
				// 第二个桶仍构造成员，只跳过提供商 JSON 与全局提供商键。
				total += len(schedulerSnapshotMembers(ids))
				schedulerCachePayloadBenchmarkSink = total
			}
		})
		b.Run(fmt.Sprintf("first_baseline_%d_providers", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				total, err := benchmarkSchedulerLegacySnapshotPayload(providers)
				if err != nil {
					b.Fatal(err)
				}
				schedulerCachePayloadBenchmarkSink = total
			}
		})
		b.Run(fmt.Sprintf("first_reuse_%d_providers", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ids, total, err := benchmarkSchedulerReusableSnapshotPayload(providers)
				if err != nil {
					b.Fatal(err)
				}
				total += len(ids)
				schedulerCachePayloadBenchmarkSink = total
			}
		})
	}
}

func benchmarkSchedulerLegacySnapshotPayload(providers []providercore.Record) (int, error) {
	cacheable := make([]providercore.Record, 0, len(providers))
	total := 0
	for _, provider := range providers {
		full, meta, err := marshalSchedulerCacheProvider(provider)
		if err != nil {
			continue
		}
		total += len(full) + len(meta)
		cacheable = append(cacheable, provider)
	}
	members := make([]redis.Z, 0, len(cacheable))
	for idx, provider := range cacheable {
		members = append(members, redis.Z{Score: float64(idx), Member: strconv.FormatInt(provider.ID, 10)})
	}
	return total + len(members), nil
}

func benchmarkSchedulerReusableSnapshotPayload(providers []providercore.Record) ([]int64, int, error) {
	providerIDs := make([]int64, 0, len(providers))
	total := 0
	for _, provider := range providers {
		full, meta, err := marshalSchedulerCacheProvider(provider)
		if err != nil {
			continue
		}
		total += len(full) + len(meta)
		providerIDs = append(providerIDs, provider.ID)
	}
	total += len(schedulerSnapshotMembers(providerIDs))
	return providerIDs, total, nil
}

func schedulerCacheBenchmarkProviders(size int) []providercore.Record {
	largeValue := strings.Repeat("x", 4096)
	credentials := map[string]any{
		"api_key":       "benchmark-key",
		"model_mapping": map[string]any{"z-model": "z-target", "a-model": "a-target"},
		"large_value":   largeValue,
	}
	extra := map[string]any{
		"mixed_scheduling": true,
		"model_rate_limits": map[string]any{
			"z-model": map[string]any{"rate_limit_reset_at": "2026-07-16T00:00:00Z"},
			"a-model": map[string]any{"rate_limit_reset_at": "2026-07-16T00:00:00Z"},
		},
		"large_value": largeValue,
	}
	providers := make([]providercore.Record, size)
	for i := range providers {
		id := int64(i + 1)
		providers[i] = providercore.Record{
			ID:          id,
			Name:        "benchmark-provider",
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Credentials: credentials,
			Extra:       extra,
			GroupIDs:    []int64{7, 9},
			ProviderGroups: []providercore.GroupMembership{
				{ProviderID: id, GroupID: 7},
				{ProviderID: id, GroupID: 9},
			},
		}
	}
	return providers
}

// TestBuildSchedulerMetadataProvider_KeepsExplicitModelScopeForPassthrough 验证调度快照同时保留传输开关与明确模型范围；透传不会绕过白名单。
func TestBuildSchedulerMetadataProvider_KeepsExplicitModelScopeForPassthrough(t *testing.T) {
	for _, key := range []string{"openai_passthrough", "openai_oauth_passthrough"} {
		t.Run(key, func(t *testing.T) {
			provider := providercore.Record{
				ID:       383,
				Platform: capability.PlatformOpenAI,
				Type:     capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					// 提供商从白名单模式切到透传后常见的残留映射，未列出请求的模型。
					"model_whitelist": []string{"gpt-5.5"},
					"model_mapping":   map[string]any{"gpt-5.5": "gpt-5.5"},
					"access_token":    "drop-me",
				},
				Extra: map[string]any{key: true},
			}
			require.False(t, provider.IsModelSupported("gpt-5.6-sol", provideradapter.ModelDefaults(), provideradapter.ModelRules(&provider)),
				"透传提供商仍受明确的模型范围限制")

			meta := buildSchedulerMetadataProvider(provider)

			// 写入 sched:meta 后由 decodeCachedProvider 读回，检查序列化后的模型范围。
			payload, err := codec.MarshalProviderRecord(&meta)
			require.NoError(t, err)
			restored, err := codec.UnmarshalProviderRecord(payload)
			require.NoError(t, err)

			require.Equal(t, true, restored.Extra[key])
			require.True(t, restored.IsOpenAIPassthroughEnabled())
			require.False(t, restored.IsModelSupported("gpt-5.6-sol", provideradapter.ModelDefaults(), provideradapter.ModelRules(restored)),
				"缓存恢复不能放宽提供商模型范围")
			// 白名单本身仍需保留：非透传提供商依赖它做模型门。
			require.Equal(t, map[string]any{"gpt-5.5": "gpt-5.5"}, restored.Credentials["model_mapping"])
		})
	}
}

// TestSchedulerProtocolProjection 检查轻量和完整提供商快照都保存支持的协议集合及认证方式。
func TestSchedulerProtocolProjection(t *testing.T) {
	provider := providercore.Record{ID: 72, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{"upstream_protocols": []string{"openai_responses_websocket"}, "auth_mode": "personalAccessToken", "access_token": "hidden"}}
	metadata := buildSchedulerMetadataProvider(provider)
	require.Equal(t, []protocol.ProtocolID{"openai_responses_websocket"}, metadata.UpstreamProtocols())
	require.True(t, metadata.IsOpenAIPersonalAccessToken())
	require.NotContains(t, metadata.Credentials, "access_token")
	require.NotContains(t, metadata.NativeProtocolOptions(), protocol.ProtocolID("openai_live"))
	provider.Credentials["upstream_protocols"] = []string{}
	metadata = buildSchedulerMetadataProvider(provider)
	require.Empty(t, metadata.UpstreamProtocols())
}
