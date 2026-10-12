package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// UserStats 用户使用统计。
type UserStats struct {
	TotalRequests   int64   `json:"total_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCost       float64 `json:"total_cost"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
}

// DashboardStats 仪表盘统计。
type DashboardStats = usage.DashboardStats

// UserDashboardStats 用户仪表盘统计。
type UserDashboardStats = usage.UserDashboardStats

// getPerformanceStats 获取 RPM 和 TPM（近 5 分钟平均值，可选纳入 Owner 团队）。
func (r *Store) getPerformanceStats(ctx context.Context, userID int64, includeOwnedTeam bool) (rpm, tpm int64, err error) {
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	args := []any{fiveMinutesAgo}
	source, scopeCondition, args, err := r.buildUsageLogScopeSource(ctx, args, userID, includeOwnedTeam, "")
	if err != nil {
		return 0, 0, err
	}
	query := `
		SELECT
			COUNT(*) as request_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as token_count
		FROM ` + source + `
		WHERE created_at >= $1`
	if scopeCondition != "" {
		query += " AND " + scopeCondition
	}

	var requestCount int64
	var tokenCount int64
	if err := scanSingleRow(ctx, r.sql, query, args, &requestCount, &tokenCount); err != nil {
		return 0, 0, err
	}
	return requestCount / 5, tokenCount / 5, nil
}

func (r *Store) GetUserStats(ctx context.Context, userID int64, startTime, endTime time.Time) (*UserStats, error) {
	query := `
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as total_tokens,
			COALESCE(SUM(actual_cost), 0) as total_cost,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens
		FROM usage_logs
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
	`

	stats := &UserStats{}
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{userID, startTime, endTime},
		&stats.TotalRequests,
		&stats.TotalTokens,
		&stats.TotalCost,
		&stats.InputTokens,
		&stats.OutputTokens,
		&stats.CacheReadTokens,
	); err != nil {
		return nil, err
	}
	return stats, nil
}

// runDashboardQueries 仅在连接池上并行查询；事务等单连接执行器必须串行使用。
func (r *Store) runDashboardQueries(ctx context.Context, queries ...func(context.Context) error) error {
	if r.db == nil {
		for _, query := range queries {
			if err := query(ctx); err != nil {
				return err
			}
		}
		return nil
	}

	group, groupCtx := errgroup.WithContext(ctx)
	for _, query := range queries {
		group.Go(func() error { return query(groupCtx) })
	}
	return group.Wait()
}

func (r *Store) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	if r.preAggregation != nil && !r.preAggregation.UsageEnabled(ctx) {
		return r.GetDashboardStatsWithRange(ctx, time.Unix(0, 0), time.Now())
	}
	stats := &DashboardStats{}
	now := r.calendar.Now()
	todayStart := r.calendar.Today()

	// 实体、聚合用量和近五分钟性能彼此独立，并行读取可缩短接口关键路径。
	var rpm, tpm int64
	err := r.runDashboardQueries(
		ctx,
		func(queryCtx context.Context) error {
			return r.fillDashboardEntityStats(queryCtx, stats, todayStart, now)
		},
		func(queryCtx context.Context) error {
			return r.fillDashboardUsageStatsAggregated(queryCtx, stats, todayStart, now)
		},
		func(queryCtx context.Context) error {
			var err error
			rpm, tpm, err = r.getPerformanceStats(queryCtx, 0, false)
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	stats.Rpm = rpm
	stats.Tpm = tpm

	return stats, nil
}

func (r *Store) GetDashboardStatsWithRange(ctx context.Context, start, end time.Time) (*DashboardStats, error) {
	startUTC := start.UTC()
	endUTC := end.UTC()
	if !endUTC.After(startUTC) {
		return nil, errors.New("统计时间范围无效")
	}

	stats := &DashboardStats{}
	now := r.calendar.Now()
	todayStart := r.calendar.Today()

	if err := r.fillDashboardEntityStats(ctx, stats, todayStart, now); err != nil {
		return nil, err
	}
	if err := r.fillDashboardUsageStatsFromUsageLogs(ctx, stats, startUTC, endUTC, todayStart, now); err != nil {
		return nil, err
	}

	rpm, tpm, err := r.getPerformanceStats(ctx, 0, false)
	if err != nil {
		return nil, err
	}
	stats.Rpm = rpm
	stats.Tpm = tpm

	return stats, nil
}

func (r *Store) fillDashboardEntityStats(ctx context.Context, stats *DashboardStats, todayUTC, now time.Time) error {
	userStatsQuery := `
		SELECT
			COUNT(*) as total_users,
			COUNT(CASE WHEN created_at >= $1 THEN 1 END) as today_new_users
		FROM users
		WHERE deleted_at IS NULL
	`
	apiKeyStatsQuery := `
		SELECT
			COUNT(*) as total_api_keys,
			COUNT(CASE WHEN status = $1 THEN 1 END) as active_api_keys
		FROM api_keys
		WHERE deleted_at IS NULL
	`
	providerStatsQuery := `
		SELECT
			COUNT(*) as total_providers,
			COUNT(CASE WHEN status = $1 AND schedulable = true THEN 1 END) as normal_providers,
			COUNT(CASE WHEN status = $2 THEN 1 END) as error_providers,
			COUNT(CASE WHEN rate_limited_at IS NOT NULL AND rate_limit_reset_at > $3 THEN 1 END) as ratelimit_providers,
			COUNT(CASE WHEN overload_until IS NOT NULL AND overload_until > $4 THEN 1 END) as overload_providers
		FROM providers
		WHERE deleted_at IS NULL
	`
	return r.runDashboardQueries(
		ctx,
		func(queryCtx context.Context) error {
			return scanSingleRow(queryCtx, r.sql, userStatsQuery, []any{todayUTC}, &stats.TotalUsers, &stats.TodayNewUsers)
		},
		func(queryCtx context.Context) error {
			return scanSingleRow(queryCtx, r.sql, apiKeyStatsQuery, []any{identity.StatusActive}, &stats.TotalAPIKeys, &stats.ActiveAPIKeys)
		},
		func(queryCtx context.Context) error {
			return scanSingleRow(
				queryCtx, r.sql, providerStatsQuery,
				[]any{identity.StatusActive, provider.StatusError, now, now},
				&stats.TotalProviders, &stats.NormalProviders, &stats.ErrorProviders,
				&stats.RateLimitProviders, &stats.OverloadProviders,
			)
		},
	)
}

func (r *Store) fillDashboardUsageStatsAggregated(ctx context.Context, stats *DashboardStats, todayUTC, now time.Time) error {
	combinedStatsQuery := `
		SELECT
			COALESCE(SUM(total_requests), 0), COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cache_creation_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(actual_cost), 0), COALESCE(SUM(provider_cost), 0),
			COALESCE(SUM(total_duration_ms), 0),
			COALESCE(SUM(total_requests) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(SUM(input_tokens) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(SUM(output_tokens) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(SUM(total_cost) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(SUM(actual_cost) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(SUM(provider_cost) FILTER (WHERE bucket_date = $1::date), 0),
			COALESCE(MAX(active_users) FILTER (WHERE bucket_date = $1::date), 0)
		FROM usage_dashboard_daily
	`
	var totalDurationMs int64
	combinedStats := func(queryCtx context.Context) error {
		return scanSingleRow(
			queryCtx, r.sql, combinedStatsQuery, []any{todayUTC},
			&stats.TotalRequests,
			&stats.TotalInputTokens,
			&stats.TotalOutputTokens,
			&stats.TotalCacheCreationTokens,
			&stats.TotalCacheReadTokens,
			&stats.TotalCost,
			&stats.TotalActualCost,
			&stats.TotalProviderCost,
			&totalDurationMs,
			&stats.TodayRequests,
			&stats.TodayInputTokens,
			&stats.TodayOutputTokens,
			&stats.TodayCacheCreationTokens,
			&stats.TodayCacheReadTokens,
			&stats.TodayCost,
			&stats.TodayActualCost,
			&stats.TodayProviderCost,
			&stats.ActiveUsers,
		)
	}

	hourlyActiveQuery := `
		SELECT active_users
		FROM usage_dashboard_hourly
		WHERE bucket_start = $1
	`
	hourStart := now.In(r.calendar.Location()).Truncate(time.Hour)
	hourlyActive := func(queryCtx context.Context) error {
		if err := scanSingleRow(queryCtx, r.sql, hourlyActiveQuery, []any{hourStart}, &stats.HourlyActiveUsers); err != nil {
			if err != sql.ErrNoRows {
				return err
			}
		}
		return nil
	}
	if err := r.runDashboardQueries(ctx, combinedStats, hourlyActive); err != nil {
		return err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens
	if stats.TotalRequests > 0 {
		stats.AverageDurationMs = float64(totalDurationMs) / float64(stats.TotalRequests)
	}

	return nil
}

func (r *Store) fillDashboardUsageStatsFromUsageLogs(ctx context.Context, stats *DashboardStats, startUTC, endUTC, todayUTC, now time.Time) error {
	todayEnd := todayUTC.Add(24 * time.Hour)
	combinedStatsQuery := `
		WITH scoped AS (
			SELECT
				created_at,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				total_cost,
				actual_cost,
				COALESCE(provider_stats_cost, total_cost) * COALESCE(provider_rate_multiplier, 1) AS provider_cost,
				COALESCE(duration_ms, 0) AS duration_ms
			FROM usage_logs
			WHERE created_at >= LEAST($1::timestamptz, $3::timestamptz)
				AND created_at < GREATEST($2::timestamptz, $4::timestamptz)
		)
		SELECT
			COUNT(*) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz) AS total_requests,
			COALESCE(SUM(input_tokens) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_input_tokens,
			COALESCE(SUM(output_tokens) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_output_tokens,
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_cache_read_tokens,
			COALESCE(SUM(total_cost) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_actual_cost,
			COALESCE(SUM(provider_cost) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_provider_cost,
			COALESCE(SUM(duration_ms) FILTER (WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz), 0) AS total_duration_ms,
			COUNT(*) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz) AS today_requests,
			COALESCE(SUM(input_tokens) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz), 0) AS today_input_tokens,
			COALESCE(SUM(output_tokens) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz), 0) AS today_output_tokens,
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz), 0) AS today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz), 0) AS today_cache_read_tokens,
			COALESCE(SUM(total_cost) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz), 0) AS today_cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz), 0) AS today_actual_cost,
			COALESCE(SUM(provider_cost) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz), 0) AS today_provider_cost
		FROM scoped
	`
	var totalDurationMs int64
	if err := scanSingleRow(
		ctx,
		r.sql,
		combinedStatsQuery,
		[]any{startUTC, endUTC, todayUTC, todayEnd},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.TotalProviderCost,
		&totalDurationMs,
		&stats.TodayRequests,
		&stats.TodayInputTokens,
		&stats.TodayOutputTokens,
		&stats.TodayCacheCreationTokens,
		&stats.TodayCacheReadTokens,
		&stats.TodayCost,
		&stats.TodayActualCost,
		&stats.TodayProviderCost,
	); err != nil {
		return err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	if stats.TotalRequests > 0 {
		stats.AverageDurationMs = float64(totalDurationMs) / float64(stats.TotalRequests)
	}

	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens

	hourStart := now.UTC().Truncate(time.Hour)
	hourEnd := hourStart.Add(time.Hour)
	activeUsersQuery := `
		WITH scoped AS (
			SELECT user_id, created_at
			FROM usage_logs
			WHERE created_at >= LEAST($1::timestamptz, $3::timestamptz)
				AND created_at < GREATEST($2::timestamptz, $4::timestamptz)
		)
		SELECT
			COUNT(DISTINCT CASE WHEN created_at >= $1::timestamptz AND created_at < $2::timestamptz THEN user_id END) AS active_users,
			COUNT(DISTINCT CASE WHEN created_at >= $3::timestamptz AND created_at < $4::timestamptz THEN user_id END) AS hourly_active_users
		FROM scoped
	`
	return scanSingleRow(ctx, r.sql, activeUsersQuery, []any{todayUTC, todayEnd, hourStart, hourEnd}, &stats.ActiveUsers, &stats.HourlyActiveUsers)
}

// GetUserDashboardStats 获取用户专属的仪表盘统计。
func (r *Store) GetUserDashboardStats(ctx context.Context, userID int64) (*UserDashboardStats, error) {
	if stats, ok, err := r.getUserDashboardStatsFromAnalytics(ctx, userID); err == nil && ok {
		return stats, nil
	} else if err != nil {
		r.logUsageAnalyticsFallback("user_dashboard", err)
	}
	stats := &UserDashboardStats{}
	today := r.calendar.Today()
	teamID, err := r.getOwnedTeamID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 用户和 Owner 团队分别使用索引扫描；第二分支排除用户本人，保证只统计一次。
	loadAPIKeyStats := func(queryCtx context.Context) error {
		return scanSingleRow(queryCtx, r.sql, `
			WITH scoped AS (
				SELECT status FROM api_keys
				WHERE deleted_at IS NULL AND user_id = $1
				UNION ALL
				SELECT status FROM api_keys
				WHERE deleted_at IS NULL AND $2 > 0 AND team_id = $2 AND user_id <> $1
			)
			SELECT COUNT(*), COUNT(*) FILTER (WHERE status = $3) FROM scoped
		`, []any{userID, teamID, identity.StatusActive}, &stats.TotalAPIKeys, &stats.ActiveAPIKeys)
	}

	// 累计和今日统计共用同一组可索引范围，避免重复扫描大型使用记录表。
	var totalDuration, durationCount int64
	usageStatsQuery := `
		WITH scoped AS (
			SELECT created_at, input_tokens, output_tokens, cache_creation_tokens,
			       cache_read_tokens, total_cost, actual_cost, duration_ms
			FROM usage_logs WHERE user_id = $1
			UNION ALL
			SELECT created_at, input_tokens, output_tokens, cache_creation_tokens,
			       cache_read_tokens, total_cost, actual_cost, duration_ms
			FROM usage_logs
			WHERE $2 > 0 AND team_id = $2 AND user_id <> $1
		)
		SELECT
			COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost), 0), COALESCE(SUM(actual_cost), 0),
			COALESCE(SUM(COALESCE(duration_ms, 0)), 0), COUNT(duration_ms),
			COUNT(*) FILTER (WHERE created_at >= $3),
			COALESCE(SUM(input_tokens) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(output_tokens) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(total_cost) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $3), 0)
		FROM scoped
	`
	loadUsageStats := func(queryCtx context.Context) error {
		return scanSingleRow(
			queryCtx,
			r.sql,
			usageStatsQuery,
			[]any{userID, teamID, today},
			&stats.TotalRequests,
			&stats.TotalInputTokens,
			&stats.TotalOutputTokens,
			&stats.TotalCacheCreationTokens,
			&stats.TotalCacheReadTokens,
			&stats.TotalCost,
			&stats.TotalActualCost,
			&totalDuration,
			&durationCount,
			&stats.TodayRequests,
			&stats.TodayInputTokens,
			&stats.TodayOutputTokens,
			&stats.TodayCacheCreationTokens,
			&stats.TodayCacheReadTokens,
			&stats.TodayCost,
			&stats.TodayActualCost,
		)
	}

	// 近五分钟窗口有独立时间索引，与累计统计并行执行。
	var rpm, tpm int64
	loadPerformanceStats := func(queryCtx context.Context) error {
		var performanceErr error
		rpm, tpm, performanceErr = r.getPerformanceStatsForScope(queryCtx, userID, teamID)
		return performanceErr
	}
	if err := r.runDashboardQueries(ctx, loadAPIKeyStats, loadUsageStats, loadPerformanceStats); err != nil {
		return nil, err
	}
	if durationCount > 0 {
		stats.AverageDurationMs = float64(totalDuration) / float64(durationCount)
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens

	stats.Rpm = rpm
	stats.Tpm = tpm

	return stats, nil
}

// getPerformanceStatsByAPIKey 获取指定 API Key 的 RPM 和 TPM（近 5 分钟平均值）。
func (r *Store) getPerformanceStatsByAPIKey(ctx context.Context, apiKeyID int64) (rpm, tpm int64, err error) {
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	query := `
		SELECT
			COUNT(*) as request_count,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as token_count
		FROM usage_logs
		WHERE created_at >= $1 AND api_key_id = $2`
	args := []any{fiveMinutesAgo, apiKeyID}

	var requestCount int64
	var tokenCount int64
	if err := scanSingleRow(ctx, r.sql, query, args, &requestCount, &tokenCount); err != nil {
		return 0, 0, err
	}
	return requestCount / 5, tokenCount / 5, nil
}

// GetAPIKeyDashboardStats 获取指定 API Key 的仪表盘统计（按 api_key_id 过滤）。
func (r *Store) GetAPIKeyDashboardStats(ctx context.Context, apiKeyID int64) (*UserDashboardStats, error) {
	stats, aggregated, err := r.getAPIKeyDashboardStatsFromAnalytics(ctx, apiKeyID)
	if err != nil {
		r.logUsageAnalyticsFallback("api_key_dashboard", err)
	}
	if err != nil || !aggregated {
		stats = &UserDashboardStats{}
		// 累计和今日共用一次原始记录扫描，未来时间记录也参与统计。
		query := `SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(total_cost), 0), COALESCE(SUM(actual_cost), 0), COALESCE(AVG(duration_ms), 0),
			COUNT(*) FILTER (WHERE created_at >= $2),
			COALESCE(SUM(input_tokens) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(output_tokens) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(total_cost) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $2), 0)
			FROM usage_logs WHERE api_key_id = $1`
		if err := scanSingleRow(ctx, r.sql, query, []any{apiKeyID, r.calendar.Today()},
			&stats.TotalRequests, &stats.TotalInputTokens, &stats.TotalOutputTokens,
			&stats.TotalCacheCreationTokens, &stats.TotalCacheReadTokens,
			&stats.TotalCost, &stats.TotalActualCost, &stats.AverageDurationMs,
			&stats.TodayRequests, &stats.TodayInputTokens, &stats.TodayOutputTokens,
			&stats.TodayCacheCreationTokens, &stats.TodayCacheReadTokens,
			&stats.TodayCost, &stats.TodayActualCost); err != nil {
			return nil, err
		}
	}
	stats.TotalAPIKeys = 1
	stats.ActiveAPIKeys = 1
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens
	// 短窗口读取原始记录，反映当前请求速率。
	rpm, tpm, err := r.getPerformanceStatsByAPIKey(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	stats.Rpm = rpm
	stats.Tpm = tpm
	return stats, nil
}
