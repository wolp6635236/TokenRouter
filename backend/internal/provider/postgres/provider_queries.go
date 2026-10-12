package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/lib/pq"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	dbpredicate "github.com/TokenFlux/TokenRouter/ent/predicate"
	dbprovider "github.com/TokenFlux/TokenRouter/ent/provider"
	dbprovidergroup "github.com/TokenFlux/TokenRouter/ent/providergroup"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	acctcore "github.com/TokenFlux/TokenRouter/internal/provider"
)

type ProviderGroupQueryOptions struct {
	status               string
	schedulable          bool
	ignoreTransientState bool
	platforms            []string // 允许的多个平台，空切片表示不进行平台过滤
}

func (r *ProviderStore) GetByCRSAccountID(ctx context.Context, crsProviderID string) (*acctcore.Record, error) {
	if crsProviderID == "" {
		return nil, nil
	}

	// 使用 sqljson.ValueEQ 生成 JSON 路径过滤。
	// CRS 查询只匹配母提供商；即使影子的 Extra 含有 crs_account_id，也不能让同步覆盖其类型、凭据或代理。
	m, err := r.client.Provider.Query().
		Where(dbprovider.ParentProviderIDIsNil()).
		Where(func(s *entsql.Selector) {
			s.Where(sqljson.ValueEQ(dbprovider.FieldExtra, crsProviderID, sqljson.Path("crs_account_id")))
		}).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	providers, err := r.RecordsFromEntities(ctx, []*dbent.Provider{m})
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, nil
	}
	return &providers[0], nil
}

// FindByExtraField 根据 extra 字段中的键值对查找提供商。
// 使用 PostgreSQL JSONB @> 操作符进行高效查询（需要 GIN 索引支持）。
//
// FindByExtraField finds providers by key-value pairs in the extra field.
// Uses PostgreSQL JSONB @> operator for efficient queries (requires GIN index).
func (r *ProviderStore) FindByExtraField(ctx context.Context, key string, value any) ([]acctcore.Record, error) {
	providers, err := r.client.Provider.Query().
		Where(
			dbprovider.DeletedAtIsNil(),
			func(s *entsql.Selector) {
				path := sqljson.Path(key)
				switch v := value.(type) {
				case string:
					preds := []*entsql.Predicate{sqljson.ValueEQ(dbprovider.FieldExtra, v, path)}
					if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
						preds = append(preds, sqljson.ValueEQ(dbprovider.FieldExtra, parsed, path))
					}
					if len(preds) == 1 {
						s.Where(preds[0])
					} else {
						s.Where(entsql.Or(preds...))
					}
				case int:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbprovider.FieldExtra, v, path),
						sqljson.ValueEQ(dbprovider.FieldExtra, strconv.Itoa(v), path),
					))
				case int64:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbprovider.FieldExtra, v, path),
						sqljson.ValueEQ(dbprovider.FieldExtra, strconv.FormatInt(v, 10), path),
					))
				case json.Number:
					if parsed, err := v.Int64(); err == nil {
						s.Where(entsql.Or(
							sqljson.ValueEQ(dbprovider.FieldExtra, parsed, path),
							sqljson.ValueEQ(dbprovider.FieldExtra, v.String(), path),
						))
					} else {
						s.Where(sqljson.ValueEQ(dbprovider.FieldExtra, v.String(), path))
					}
				default:
					s.Where(sqljson.ValueEQ(dbprovider.FieldExtra, value, path))
				}
			},
		).
		All(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, acctcore.ErrProviderNotFound, nil)
	}

	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	// CRS 同步映射记录母提供商，影子的类型、凭据和代理由继承规则管理。
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, extra->>'crs_account_id'
		FROM providers
		WHERE deleted_at IS NULL
			AND parent_provider_id IS NULL
			AND extra->>'crs_account_id' IS NOT NULL
			AND extra->>'crs_account_id' != ''
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]int64)
	for rows.Next() {
		var id int64
		var crsID string
		if err := rows.Scan(&id, &crsID); err != nil {
			return nil, err
		}
		result[crsID] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ListWithFilters 按分页参数和管理端筛选条件查询提供商。
func (r *ProviderStore) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, providerType, status, search string, groupID int64, privacyMode string) ([]acctcore.Record, *pagination.PaginationResult, error) {
	q := r.ProviderListFilteredQuery(platform, providerType, status, search, groupID, privacyMode)
	// Count 使用查询构造器的副本，SoftDeleteMixin 等拦截器只向副本添加谓词。
	// 后续列表查询据此与总数使用相同条件。
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	providersQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range ProviderListOrder(params) {
		providersQuery = providersQuery.Order(order)
	}

	providers, err := providersQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outProviders, err := r.RecordsFromEntities(ctx, providers)
	if err != nil {
		return nil, nil, err
	}
	return outProviders, pagination.ResultFromTotal(int64(total), params), nil
}

// ListAllWithFilters 查询符合管理端筛选条件的全部提供商，供调度评分使用。
func (r *ProviderStore) ListAllWithFilters(ctx context.Context, platform, providerType, status, search string, groupID int64, privacyMode string) ([]acctcore.Record, error) {
	providers, err := r.ProviderListFilteredQuery(platform, providerType, status, search, groupID, privacyMode).All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

// ListOpsProvidersForStats 仅加载实时运维统计需要的提供商字段和分组关系。
func (r *ProviderStore) ListOpsProvidersForStats(ctx context.Context, platformFilter string, groupIDFilter *int64) ([]acctcore.Record, error) {
	if r == nil || r.client == nil {
		return []acctcore.Record{}, nil
	}

	q := r.client.Provider.Query()
	if platformFilter = strings.TrimSpace(platformFilter); platformFilter != "" {
		q = q.Where(dbprovider.PlatformEQ(platformFilter))
	}
	if groupIDFilter != nil && *groupIDFilter > 0 {
		q = q.Where(dbprovider.HasProviderGroupsWith(dbprovidergroup.GroupIDEQ(*groupIDFilter)))
	}

	providers, err := q.
		Select(
			dbprovider.FieldID,
			dbprovider.FieldName,
			dbprovider.FieldPlatform,
			dbprovider.FieldConcurrency,
			dbprovider.FieldLoadFactor,
			dbprovider.FieldStatus,
			dbprovider.FieldErrorMessage,
			dbprovider.FieldSchedulable,
			dbprovider.FieldRateLimitResetAt,
			dbprovider.FieldOverloadUntil,
			dbprovider.FieldTempUnschedulableUntil,
		).
		Order(dbent.Asc(dbprovider.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListByGroup(ctx context.Context, groupID int64) ([]acctcore.Record, error) {
	providers, err := r.QueryProvidersByGroup(ctx, groupID, ProviderGroupQueryOptions{
		status: acctcore.StatusActive,
	})
	if err != nil {
		return nil, err
	}
	return providers, nil
}

func (r *ProviderStore) ListActive(ctx context.Context) ([]acctcore.Record, error) {
	providers, err := r.client.Provider.Query().
		Where(dbprovider.StatusEQ(acctcore.StatusActive)).
		Order(dbent.Asc(dbprovider.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListOAuthRefreshCandidatePage(ctx context.Context, options acctcore.OAuthRefreshPageOptions) (*acctcore.OAuthRefreshCandidatePage, error) {
	if r.sql == nil {
		return nil, errors.New("provider repository SQL executor not configured")
	}
	if len(options.Platforms) == 0 {
		return nil, errors.New("oauth refresh candidate platforms cannot be empty")
	}
	if options.Limit <= 0 || options.Limit > 1000 {
		return nil, errors.New("oauth refresh candidate page limit must be between 1 and 1000")
	}

	// (cond) IS NOT TRUE 把 NULL 和 FALSE 都视为"可被刷新"。直接写
	// NOT (a AND b) 在 PG 三值逻辑下会把 a 或 b 为 NULL 的行（即绝大多数
	// 健康提供商：temp_unschedulable_until=NULL）也排除，导致后台 token
	// 刷新工作器漏掉所有正常提供商，access_token 到期后请求开始 401。
	// schedulable=false 表示永久停用，只排除该状态；临时不可调度仍由下方冷却条件处理。
	query := `
		SELECT id
		FROM providers
		WHERE deleted_at IS NULL
			AND schedulable = TRUE
			AND platform = ANY($1)
			AND id > $2`
	if options.ActiveOnly {
		query += `
			AND status = 'active'`
	}
	if options.IncludeSetupToken {
		query += `
			AND (type IN ('oauth', 'setup-token') OR (platform = 'qoder' AND type = 'cosy'))`
	} else {
		query += `
			AND (type = 'oauth' OR (platform = 'qoder' AND type = 'cosy'))`
	}
	if options.RequireRefreshToken {
		query += `
			AND credentials ? 'refresh_token'
			AND btrim(credentials->>'refresh_token') <> ''`
	}
	if options.ExcludeRetryCooldown {
		query += `
			AND (
				temp_unschedulable_until > NOW()
				AND temp_unschedulable_reason LIKE 'token refresh retry exhausted:%'
			) IS NOT TRUE`
	}
	query += `
		ORDER BY id ASC
		LIMIT $3`

	rows, err := r.sql.QueryContext(ctx, query, pq.Array(options.Platforms), options.AfterID, options.Limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &acctcore.OAuthRefreshCandidatePage{Providers: []acctcore.Record{}}, nil
	}

	providers, err := r.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	providersByID := make(map[int64]*acctcore.Record, len(providers))
	for _, provider := range providers {
		if provider != nil {
			providersByID[provider.ID] = provider
		}
	}
	out := make([]acctcore.Record, 0, len(providers))
	for _, id := range ids {
		if provider := providersByID[id]; provider != nil {
			out = append(out, *provider)
		}
	}
	page := &acctcore.OAuthRefreshCandidatePage{
		Providers: out,
		HasMore:   len(ids) == options.Limit,
	}
	if len(ids) > 0 {
		page.NextAfterID = ids[len(ids)-1]
	}
	return page, nil
}

func (r *ProviderStore) ListByPlatform(ctx context.Context, platform string) ([]acctcore.Record, error) {
	providers, err := r.client.Provider.Query().
		Where(
			dbprovider.PlatformEQ(platform),
			dbprovider.StatusEQ(acctcore.StatusActive),
		).
		Order(dbent.Asc(dbprovider.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListSchedulable(ctx context.Context) ([]acctcore.Record, error) {
	providers, err := r.SchedulableProvidersQuery(time.Now()).All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]acctcore.Record, error) {
	return r.QueryProvidersByGroup(ctx, groupID, ProviderGroupQueryOptions{
		status:      acctcore.StatusActive,
		schedulable: true,
	})
}

// ListSchedulableCapacityByGroupIDs 批量返回分组容量汇总所需的轻量提供商字段。
func (r *ProviderStore) ListSchedulableCapacityByGroupIDs(ctx context.Context, groupIDs []int64) ([]acctcore.GroupProviderCapacityRow, error) {
	groupIDs = UniquePositiveInt64s(groupIDs)
	if len(groupIDs) == 0 {
		return []acctcore.GroupProviderCapacityRow{}, nil
	}
	if r.sql == nil {
		rows := make([]acctcore.GroupProviderCapacityRow, 0)
		for _, groupID := range groupIDs {
			providers, err := r.ListSchedulableByGroupID(ctx, groupID)
			if err != nil {
				return nil, err
			}
			for i := range providers {
				acc := &providers[i]
				rows = append(rows, acctcore.GroupProviderCapacityRow{
					GroupID:             groupID,
					ProviderID:          acc.ID,
					Platform:            acc.Platform,
					Concurrency:         acc.Concurrency,
					Extra:               acctcore.CloneValues(acc.Extra),
					SessionWindowStart:  acc.SessionWindowStart,
					SessionWindowEnd:    acc.SessionWindowEnd,
					SessionWindowStatus: acc.SessionWindowStatus,
				})
			}
		}
		return rows, nil
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT
			ag.group_id,
			a.id AS provider_id,
			a.platform,
			a.concurrency,
			COALESCE(a.extra, '{}'::jsonb)::text AS extra,
			a.session_window_start,
			a.session_window_end,
			COALESCE(a.session_window_status, '') AS session_window_status
		FROM provider_groups ag
		JOIN providers a ON a.id = ag.provider_id
		WHERE ag.group_id = ANY($1)
			AND a.deleted_at IS NULL
			AND a.status = $2
			AND a.schedulable = TRUE
			AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= $3)
			AND (a.expires_at IS NULL OR a.expires_at > $3 OR a.auto_pause_on_expired = FALSE)
			AND (a.overload_until IS NULL OR a.overload_until <= $3)
			AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= $3)
		ORDER BY ag.group_id ASC, a.priority ASC, a.id ASC
	`, pq.Array(groupIDs), acctcore.StatusActive, time.Now())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]acctcore.GroupProviderCapacityRow, 0)
	for rows.Next() {
		var row acctcore.GroupProviderCapacityRow
		var extraRaw string
		if err := rows.Scan(
			&row.GroupID,
			&row.ProviderID,
			&row.Platform,
			&row.Concurrency,
			&extraRaw,
			&row.SessionWindowStart,
			&row.SessionWindowEnd,
			&row.SessionWindowStatus,
		); err != nil {
			return nil, err
		}
		if extraRaw != "" && extraRaw != "null" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(extraRaw), &extra); err != nil {
				return nil, err
			}
			row.Extra = extra
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ProviderStore) ListSchedulableByPlatform(ctx context.Context, platform string) ([]acctcore.Record, error) {
	now := time.Now()
	providers, err := r.client.Provider.Query().
		Where(
			dbprovider.PlatformEQ(platform),
			dbprovider.StatusEQ(acctcore.StatusActive),
			dbprovider.SchedulableEQ(true),
			TempUnschedulablePredicate(),
			NotExpiredPredicate(now),
			dbprovider.Or(dbprovider.OverloadUntilIsNil(), dbprovider.OverloadUntilLTE(now)),
			dbprovider.Or(dbprovider.RateLimitResetAtIsNil(), dbprovider.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbprovider.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]acctcore.Record, error) {
	// 单平台查询复用多平台逻辑，保持过滤条件与排序策略一致。
	return r.QueryProvidersByGroup(ctx, groupID, ProviderGroupQueryOptions{
		status:      acctcore.StatusActive,
		schedulable: true,
		platforms:   []string{platform},
	})
}

func (r *ProviderStore) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]acctcore.Record, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 仅返回可调度的活跃提供商，并过滤处于过载/限流窗口的提供商。
	// RecordsFromEntities 批量加载代理和分组信息。
	now := time.Now()
	providers, err := r.client.Provider.Query().
		Where(
			dbprovider.PlatformIn(platforms...),
			dbprovider.StatusEQ(acctcore.StatusActive),
			dbprovider.SchedulableEQ(true),
			TempUnschedulablePredicate(),
			NotExpiredPredicate(now),
			dbprovider.Or(dbprovider.OverloadUntilIsNil(), dbprovider.OverloadUntilLTE(now)),
			dbprovider.Or(dbprovider.RateLimitResetAtIsNil(), dbprovider.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbprovider.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]acctcore.Record, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 复用按分组查询逻辑，保证提供商全局优先级与提供商 ID 的稳定排序一致。
	return r.QueryProvidersByGroup(ctx, groupID, ProviderGroupQueryOptions{
		status:      acctcore.StatusActive,
		schedulable: true,
		platforms:   platforms,
	})
}

func (r *ProviderStore) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]acctcore.Record, error) {
	now := time.Now()
	providers, err := r.client.Provider.Query().
		Where(
			dbprovider.PlatformEQ(platform),
			dbprovider.StatusEQ(acctcore.StatusActive),
			dbprovider.SchedulableEQ(true),
			dbprovider.Not(dbprovider.HasProviderGroups()),
			TempUnschedulablePredicate(),
			NotExpiredPredicate(now),
			dbprovider.Or(dbprovider.OverloadUntilIsNil(), dbprovider.OverloadUntilLTE(now)),
			dbprovider.Or(dbprovider.RateLimitResetAtIsNil(), dbprovider.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbprovider.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

func (r *ProviderStore) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]acctcore.Record, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	now := time.Now()
	providers, err := r.client.Provider.Query().
		Where(
			dbprovider.PlatformIn(platforms...),
			dbprovider.StatusEQ(acctcore.StatusActive),
			dbprovider.SchedulableEQ(true),
			dbprovider.Not(dbprovider.HasProviderGroups()),
			TempUnschedulablePredicate(),
			NotExpiredPredicate(now),
			dbprovider.Or(dbprovider.OverloadUntilIsNil(), dbprovider.OverloadUntilLTE(now)),
			dbprovider.Or(dbprovider.RateLimitResetAtIsNil(), dbprovider.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbprovider.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

// ListModelAvailabilityCandidates 返回用于判断模型支持情况的持久配置提供商池。
// 与调度查询不同，该方法刻意忽略限流、过载、临时不可调度和到期窗口等瞬时状态。
func (r *ProviderStore) ListModelAvailabilityCandidates(
	ctx context.Context,
	groupID *int64,
	platforms []string,
	includeGrouped bool,
) ([]acctcore.Record, error) {
	if len(platforms) == 0 {
		return []acctcore.Record{}, nil
	}
	if groupID != nil {
		return r.QueryProvidersByGroup(ctx, *groupID, ProviderGroupQueryOptions{
			status:               acctcore.StatusActive,
			schedulable:          true,
			ignoreTransientState: true,
			platforms:            platforms,
		})
	}

	preds := []dbpredicate.Provider{
		dbprovider.StatusEQ(acctcore.StatusActive),
		dbprovider.SchedulableEQ(true),
		dbprovider.PlatformIn(platforms...),
	}
	if !includeGrouped {
		preds = append(preds, dbprovider.Not(dbprovider.HasProviderGroups()))
	}
	providers, err := r.client.Provider.Query().
		Where(preds...).
		Order(dbent.Asc(dbprovider.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.RecordsFromEntities(ctx, providers)
}

// ListShadowsByParent 返回指定父提供商的影子提供商；当前实现仅查 quota_dimension='spark'（唯一预设）。
// 同时过滤 parent_provider_id 和 quota_dimension='spark'，防止未来其它 linked 维度被误伤。
// ⚠️ 新增影子维度时：须更新此函数（或新增维度专用列举），并检查所有调用点（级联删除/一母一影校验/type 守卫），否则会静默漏掉新维度。
// 软删除行由 SoftDeleteMixin 拦截器自动排除，无需手写 deleted_at IS NULL。
func (r *ProviderStore) ListShadowsByParent(ctx context.Context, parentID int64) ([]*acctcore.Record, error) {
	rows, err := r.client.Provider.Query().
		Where(dbprovider.ParentProviderIDEQ(parentID), dbprovider.QuotaDimensionEQ(dbprovider.QuotaDimensionSpark)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*acctcore.Record, 0, len(rows))
	for _, m := range rows {
		out = append(out, r.recordFromEntity(m))
	}
	return out, nil
}

func (r *ProviderStore) QueryProvidersByGroup(ctx context.Context, groupID int64, opts ProviderGroupQueryOptions) ([]acctcore.Record, error) {
	q := r.client.ProviderGroup.Query().
		Where(dbprovidergroup.GroupIDEQ(groupID))

	// 通过 provider_groups 中间表查询提供商，并按需叠加状态/平台/调度能力过滤。
	preds := make([]dbpredicate.Provider, 0, 6)
	preds = append(preds, dbprovider.DeletedAtIsNil())
	if opts.status != "" {
		preds = append(preds, dbprovider.StatusEQ(opts.status))
	}
	if len(opts.platforms) > 0 {
		preds = append(preds, dbprovider.PlatformIn(opts.platforms...))
	}
	if opts.schedulable {
		preds = append(preds, dbprovider.SchedulableEQ(true))
		if !opts.ignoreTransientState {
			now := time.Now()
			preds = append(preds,
				TempUnschedulablePredicate(),
				NotExpiredPredicate(now),
				dbprovider.Or(dbprovider.OverloadUntilIsNil(), dbprovider.OverloadUntilLTE(now)),
				dbprovider.Or(dbprovider.RateLimitResetAtIsNil(), dbprovider.RateLimitResetAtLTE(now)),
			)
		}
	}

	if len(preds) > 0 {
		q = q.Where(dbprovidergroup.HasProviderWith(preds...))
	}

	groups, err := q.
		Order(
			dbprovidergroup.ByProviderField(dbprovider.FieldPriority),
			dbprovidergroup.ByProviderID(),
		).
		WithProvider().
		All(ctx)
	if err != nil {
		return nil, err
	}

	orderedIDs := make([]int64, 0, len(groups))
	providerMap := make(map[int64]*dbent.Provider, len(groups))
	for _, ag := range groups {
		if ag.Edges.Provider == nil {
			continue
		}
		if _, exists := providerMap[ag.ProviderID]; exists {
			continue
		}
		providerMap[ag.ProviderID] = ag.Edges.Provider
		orderedIDs = append(orderedIDs, ag.ProviderID)
	}

	providers := make([]*dbent.Provider, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if acc, ok := providerMap[id]; ok {
			providers = append(providers, acc)
		}
	}

	return r.RecordsFromEntities(ctx, providers)
}

func ProviderListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderAsc)

	field := dbprovider.FieldName
	defaultOrder := true
	switch sortBy {
	case "", "name":
		field = dbprovider.FieldName
	case "id":
		field = dbprovider.FieldID
		defaultOrder = false
	case "status":
		field = dbprovider.FieldStatus
		defaultOrder = false
	case "schedulable":
		field = dbprovider.FieldSchedulable
		defaultOrder = false
	case "priority":
		field = dbprovider.FieldPriority
		defaultOrder = false
	case "rate_multiplier":
		field = dbprovider.FieldRateMultiplier
		defaultOrder = false
	case "last_used_at":
		field = dbprovider.FieldLastUsedAt
		defaultOrder = false
	case "expires_at":
		field = dbprovider.FieldExpiresAt
		defaultOrder = false
	case "created_at":
		field = dbprovider.FieldCreatedAt
		defaultOrder = false
	}

	if sortOrder == pagination.SortOrderDesc {
		return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(dbprovider.FieldID)}
	}
	if defaultOrder {
		return []func(*entsql.Selector){dbent.Asc(dbprovider.FieldName), dbent.Asc(dbprovider.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(dbprovider.FieldID)}
}

func (r *ProviderStore) ProviderListFilteredQuery(platform, providerType, status, search string, groupID int64, privacyMode string) *dbent.ProviderQuery {
	q := r.client.Provider.Query()

	if platform != "" {
		q = q.Where(dbprovider.PlatformEQ(platform))
	}
	if providerType != "" {
		q = q.Where(dbprovider.TypeEQ(providerType))
	}
	if status != "" {
		switch status {
		case acctcore.StatusActive:
			q = q.Where(
				dbprovider.StatusEQ(status),
				dbprovider.SchedulableEQ(true),
				dbprovider.Or(
					dbprovider.RateLimitResetAtIsNil(),
					dbprovider.RateLimitResetAtLTE(time.Now()),
				),
				dbpredicate.Provider(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.Or(
						entsql.IsNull(col),
						entsql.LTE(col, entsql.Expr("NOW()")),
					))
				}),
			)
		case "rate_limited":
			q = q.Where(
				dbprovider.StatusEQ(acctcore.StatusActive),
				dbprovider.RateLimitResetAtGT(time.Now()),
				dbpredicate.Provider(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.Or(
						entsql.IsNull(col),
						entsql.LTE(col, entsql.Expr("NOW()")),
					))
				}),
			)
		case "temp_unschedulable":
			q = q.Where(
				dbprovider.StatusEQ(acctcore.StatusActive),
				dbprovider.Or(
					dbprovider.RateLimitResetAtIsNil(),
					dbprovider.RateLimitResetAtLTE(time.Now()),
				),
				dbpredicate.Provider(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.And(
						entsql.Not(entsql.IsNull(col)),
						entsql.GT(col, entsql.Expr("NOW()")),
					))
				}),
			)
		case "unschedulable":
			q = q.Where(
				dbprovider.StatusEQ(acctcore.StatusActive),
				dbprovider.SchedulableEQ(false),
				dbprovider.Or(
					dbprovider.RateLimitResetAtIsNil(),
					dbprovider.RateLimitResetAtLTE(time.Now()),
				),
				dbpredicate.Provider(func(s *entsql.Selector) {
					col := s.C("temp_unschedulable_until")
					s.Where(entsql.Or(
						entsql.IsNull(col),
						entsql.LTE(col, entsql.Expr("NOW()")),
					))
				}),
			)
		default:
			q = q.Where(dbprovider.StatusEQ(status))
		}
	}
	if search != "" {
		q = q.Where(dbprovider.NameContainsFold(search))
	}
	if groupID == acctcore.ProviderListGroupUngrouped {
		q = q.Where(dbprovider.Not(dbprovider.HasProviderGroups()))
	} else if groupID > 0 {
		q = q.Where(dbprovider.HasProviderGroupsWith(dbprovidergroup.GroupIDEQ(groupID)))
	}
	if privacyMode != "" {
		q = q.Where(dbpredicate.Provider(func(s *entsql.Selector) {
			path := sqljson.Path("privacy_mode")
			switch privacyMode {
			case acctcore.ProviderPrivacyModeUnsetFilter:
				s.Where(entsql.Or(
					entsql.Not(sqljson.HasKey(dbprovider.FieldExtra, path)),
					sqljson.ValueEQ(dbprovider.FieldExtra, "", path),
				))
			default:
				s.Where(sqljson.ValueEQ(dbprovider.FieldExtra, privacyMode, path))
			}
		}))
	}

	return q
}

// SchedulableProvidersQuery 为完整记录和容量摘要查询提供相同的可调度过滤条件。
func (r *ProviderStore) SchedulableProvidersQuery(now time.Time) *dbent.ProviderQuery {
	return r.client.Provider.Query().
		Where(
			dbprovider.StatusEQ(acctcore.StatusActive),
			dbprovider.SchedulableEQ(true),
			TempUnschedulablePredicate(),
			NotExpiredPredicate(now),
			dbprovider.Or(dbprovider.OverloadUntilIsNil(), dbprovider.OverloadUntilLTE(now)),
			dbprovider.Or(dbprovider.RateLimitResetAtIsNil(), dbprovider.RateLimitResetAtLTE(now)),
		).
		Order(dbent.Asc(dbprovider.FieldPriority))
}

func TempUnschedulablePredicate() dbpredicate.Provider {
	return dbpredicate.Provider(func(s *entsql.Selector) {
		col := s.C("temp_unschedulable_until")
		s.Where(entsql.Or(
			entsql.IsNull(col),
			entsql.LTE(col, entsql.Expr("NOW()")),
		))
	})
}

func NotExpiredPredicate(now time.Time) dbpredicate.Provider {
	return dbprovider.Or(
		dbprovider.ExpiresAtIsNil(),
		dbprovider.ExpiresAtGT(now),
		dbprovider.AutoPauseOnExpiredEQ(false),
	)
}
