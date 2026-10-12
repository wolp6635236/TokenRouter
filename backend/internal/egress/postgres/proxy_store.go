package postgres

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/ent/proxy"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	postgresinfra "github.com/TokenFlux/TokenRouter/internal/infra/postgres"
	logger "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
)

const proxyProviderOutboxChunkSize = 500

// ProxyProviderParticipant 不拥有事务；操作必须落到给定连接。
type ProxyProviderParticipant interface {
	InvalidateSnapshots(context.Context, int64) ([]int64, error)
	Reassign(context.Context, int64, *int64) ([]int64, error)
}
type ProxyStoreOptions struct {
	Providers func(postgresinfra.Executor) ProxyProviderParticipant
	Enqueue   func(context.Context, postgresinfra.Executor, any) error
}

// sqlQuerier 已替换为 postgresinfra.Executor（定义在 group_repo.go），
// ProxyStore 使用同一接口以支持 ExecContext。
type ProxyStore struct {
	changes ProxyStoreOptions
	client  *dbent.Client
	sql     postgresinfra.Executor
}

type ProxyConnectionIdentity = egress.ProxyConnectionIdentity

func NewProxyStore(client *dbent.Client, db postgresinfra.Executor, options ProxyStoreOptions) *ProxyStore {
	return &ProxyStore{client: client, sql: db, changes: options}
}

func (r *ProxyStore) Create(ctx context.Context, proxyIn *egress.Proxy) error {
	builder := r.client.Proxy.Create().
		SetName(proxyIn.Name).
		SetProtocol(proxyIn.Protocol).
		SetHost(proxyIn.Host).
		SetPort(proxyIn.Port).
		SetStatus(proxyIn.Status).
		SetFallbackMode(proxyIn.FallbackMode).
		SetExpiryWarnDays(proxyIn.ExpiryWarnDays)
	if proxyIn.Username != "" {
		builder.SetUsername(proxyIn.Username)
	}
	if proxyIn.Password != "" {
		builder.SetPassword(proxyIn.Password)
	}
	if proxyIn.ExpiresAt != nil {
		builder.SetExpiresAt(*proxyIn.ExpiresAt)
	}
	if proxyIn.BackupProxyID != nil {
		builder.SetBackupProxyID(*proxyIn.BackupProxyID)
	}

	created, err := builder.Save(ctx)
	if err == nil {
		applyProxyEntityToService(proxyIn, created)
	}
	return err
}

func (r *ProxyStore) GetByID(ctx context.Context, id int64) (*egress.Proxy, error) {
	m, err := r.client.Proxy.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, egress.ErrProxyNotFound
		}
		return nil, err
	}
	return ProxyEntity(m), nil
}

func (r *ProxyStore) ListByIDs(ctx context.Context, ids []int64) ([]egress.Proxy, error) {
	if len(ids) == 0 {
		return []egress.Proxy{}, nil
	}

	proxies, err := r.client.Proxy.Query().
		Where(proxy.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]egress.Proxy, 0, len(proxies))
	for i := range proxies {
		out = append(out, *ProxyEntity(proxies[i]))
	}
	return out, nil
}

func (r *ProxyStore) Update(ctx context.Context, proxyIn *egress.Proxy) error {
	client := r.client
	var tx *dbent.Tx
	if contextTx := dbent.TxFromContext(ctx); contextTx != nil {
		client = contextTx.Client()
	} else {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil && err != dbent.ErrTxStarted {
			return err
		}
		if tx != nil {
			defer func() { _ = tx.Rollback() }()
			ctx = dbent.NewTxContext(ctx, tx)
			client = tx.Client()
		}
	}

	updated, err := r.updateProxyAndInvalidateOllamaSnapshot(ctx, client, proxyIn)
	if err != nil {
		return err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	applyProxyEntityToService(proxyIn, updated)
	return nil
}

func ProxyConnectionIdentityFromProxy(proxyIn *egress.Proxy) ProxyConnectionIdentity {
	return egress.ProxyConnectionIdentityFromProxy(proxyIn)
}

func (r *ProxyStore) updateProxyAndInvalidateOllamaSnapshot(ctx context.Context, client *dbent.Client, proxyIn *egress.Proxy) (*dbent.Proxy, error) {
	currentIdentity, err := lockProxyConnectionIdentity(ctx, client, proxyIn.ID)
	if err != nil {
		return nil, err
	}
	builder := client.Proxy.UpdateOneID(proxyIn.ID).
		SetName(proxyIn.Name).
		SetProtocol(proxyIn.Protocol).
		SetHost(proxyIn.Host).
		SetPort(proxyIn.Port).
		SetStatus(proxyIn.Status).
		SetFallbackMode(proxyIn.FallbackMode).
		SetExpiryWarnDays(proxyIn.ExpiryWarnDays)
	if proxyIn.Username != "" {
		builder.SetUsername(proxyIn.Username)
	} else {
		builder.ClearUsername()
	}
	if proxyIn.Password != "" {
		builder.SetPassword(proxyIn.Password)
	} else {
		builder.ClearPassword()
	}
	if proxyIn.ExpiresAt != nil {
		builder.SetExpiresAt(*proxyIn.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if proxyIn.BackupProxyID != nil {
		builder.SetBackupProxyID(*proxyIn.BackupProxyID)
	} else {
		builder.ClearBackupProxyID()
	}

	updated, err := builder.Save(ctx)
	if dbent.IsNotFound(err) {
		return nil, egress.ErrProxyNotFound
	}
	if err != nil {
		return nil, err
	}
	if currentIdentity == ProxyConnectionIdentityFromProxy(proxyIn) {
		return updated, nil
	}
	providerIDs, err := r.changes.Providers(client).InvalidateSnapshots(ctx, proxyIn.ID)
	if err != nil {
		return nil, err
	}
	if err := r.EnqueueProviderChanges(ctx, client, providerIDs); err != nil {
		return nil, err
	}
	return updated, nil
}

func lockProxyConnectionIdentity(ctx context.Context, client *dbent.Client, proxyID int64) (ProxyConnectionIdentity, error) {
	rows, err := client.QueryContext(ctx, `
		SELECT protocol, host, port, COALESCE(username, ''), COALESCE(password, ''), status
		FROM proxies
		WHERE id = $1 AND deleted_at IS NULL
		FOR NO KEY UPDATE
	`, proxyID)
	if err != nil {
		return ProxyConnectionIdentity{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ProxyConnectionIdentity{}, err
		}
		return ProxyConnectionIdentity{}, egress.ErrProxyNotFound
	}
	var identity ProxyConnectionIdentity
	if err := rows.Scan(&identity.Protocol, &identity.Host, &identity.Port, &identity.Username, &identity.Password, &identity.Status); err != nil {
		return ProxyConnectionIdentity{}, err
	}
	return identity, rows.Err()
}

func (r *ProxyStore) EnqueueProviderChanges(ctx context.Context, exec postgresinfra.Executor, providerIDs []int64) error {
	providerIDs = SortedUniqueProviderIDs(providerIDs)
	for start := 0; start < len(providerIDs); start += proxyProviderOutboxChunkSize {
		end := min(start+proxyProviderOutboxChunkSize, len(providerIDs))
		payload := map[string]any{"provider_ids": providerIDs[start:end]}
		if err := r.changes.Enqueue(ctx, exec, payload); err != nil {
			return err
		}
	}
	return nil
}

func (r *ProxyStore) Delete(ctx context.Context, id int64) error {
	_, err := r.client.Proxy.Delete().Where(proxy.IDEQ(id)).Exec(ctx)
	return err
}

func (r *ProxyStore) List(ctx context.Context, params pagination.PaginationParams) ([]egress.Proxy, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "")
}

// ListWithFilters lists proxies with optional filtering by protocol, status, and search query.
func (r *ProxyStore) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]egress.Proxy, *pagination.PaginationResult, error) {
	q := r.client.Proxy.Query()
	if protocol != "" {
		q = q.Where(proxy.ProtocolEQ(protocol))
	}
	if status != "" {
		q = q.Where(proxy.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(proxy.NameContainsFold(search))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	proxiesQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range proxyListOrder(params) {
		proxiesQuery = proxiesQuery.Order(order)
	}

	proxies, err := proxiesQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outProxies := make([]egress.Proxy, 0, len(proxies))
	for i := range proxies {
		outProxies = append(outProxies, *ProxyEntity(proxies[i]))
	}

	return outProxies, pagination.ResultFromTotal(int64(total), params), nil
}

// ListWithFiltersAndProviderCount lists proxies with filters and includes provider count per proxy.
func (r *ProxyStore) ListWithFiltersAndProviderCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]egress.ProxyWithProviderCount, *pagination.PaginationResult, error) {
	q := r.client.Proxy.Query()
	if protocol != "" {
		q = q.Where(proxy.ProtocolEQ(protocol))
	}
	if status != "" {
		q = q.Where(proxy.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(proxy.NameContainsFold(search))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	if strings.EqualFold(strings.TrimSpace(params.SortBy), "provider_count") {
		return r.listWithProviderCountSort(ctx, q, params, total)
	}

	proxiesQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range proxyListOrder(params) {
		proxiesQuery = proxiesQuery.Order(order)
	}

	proxies, err := proxiesQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return r.buildProxyWithProviderCountResult(ctx, proxies, params, int64(total))
}

func (r *ProxyStore) listWithProviderCountSort(ctx context.Context, q *dbent.ProxyQuery, params pagination.PaginationParams, total int) ([]egress.ProxyWithProviderCount, *pagination.PaginationResult, error) {
	proxies, err := q.
		Order(dbent.Desc(proxy.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	result, _, err := r.buildProxyWithProviderCountResult(ctx, proxies, params, int64(total))
	if err != nil {
		return nil, nil, err
	}

	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].ProviderCount == result[j].ProviderCount {
			return result[i].ID > result[j].ID
		}
		if sortOrder == pagination.SortOrderAsc {
			return result[i].ProviderCount < result[j].ProviderCount
		}
		return result[i].ProviderCount > result[j].ProviderCount
	})

	return pagination.Slice(result, params), pagination.ResultFromTotal(int64(total), params), nil
}

func (r *ProxyStore) buildProxyWithProviderCountResult(ctx context.Context, proxies []*dbent.Proxy, params pagination.PaginationParams, total int64) ([]egress.ProxyWithProviderCount, *pagination.PaginationResult, error) {
	counts, err := r.GetProviderCountsForProxies(ctx)
	if err != nil {
		return nil, nil, err
	}

	result := make([]egress.ProxyWithProviderCount, 0, len(proxies))
	for i := range proxies {
		proxyOut := ProxyEntity(proxies[i])
		if proxyOut == nil {
			continue
		}
		result = append(result, egress.ProxyWithProviderCount{
			Proxy:         *proxyOut,
			ProviderCount: counts[proxyOut.ID],
		})
	}

	return result, pagination.ResultFromTotal(total, params), nil
}

func proxyListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	var field string
	switch sortBy {
	case "name":
		field = proxy.FieldName
	case "protocol":
		field = proxy.FieldProtocol
	case "status":
		field = proxy.FieldStatus
	case "created_at":
		field = proxy.FieldCreatedAt
	case "expiry":
		// expires_at 为 NULL 表示永不过期。dbent.Asc/Desc 使用 PostgreSQL 默认的 NULL 排序，
		// 升序时 NULL 在最后，降序时在最前。
		field = proxy.FieldExpiresAt
	default:
		field = proxy.FieldID
	}

	if sortOrder == pagination.SortOrderAsc {
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(proxy.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(proxy.FieldID)}
}

func (r *ProxyStore) ListActive(ctx context.Context) ([]egress.Proxy, error) {
	proxies, err := r.client.Proxy.Query().
		Where(proxy.StatusEQ(egress.StatusActive)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	outProxies := make([]egress.Proxy, 0, len(proxies))
	for i := range proxies {
		outProxies = append(outProxies, *ProxyEntity(proxies[i]))
	}
	return outProxies, nil
}

// ExistsByHostPortAuth checks if a proxy with the same host, port, username, and password exists.
func (r *ProxyStore) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	q := r.client.Proxy.Query().
		Where(proxy.HostEQ(host), proxy.PortEQ(port))

	if username == "" {
		q = q.Where(proxy.Or(proxy.UsernameIsNil(), proxy.UsernameEQ("")))
	} else {
		q = q.Where(proxy.UsernameEQ(username))
	}
	if password == "" {
		q = q.Where(proxy.Or(proxy.PasswordIsNil(), proxy.PasswordEQ("")))
	} else {
		q = q.Where(proxy.PasswordEQ(password))
	}

	count, err := q.Count(ctx)
	return count > 0, err
}

// CountProvidersByProxyID returns the number of providers using a specific proxy.
func (r *ProxyStore) CountProvidersByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	var count int64
	if err := postgresinfra.ScanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM providers WHERE proxy_id = $1 AND deleted_at IS NULL", []any{proxyID}, &count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *ProxyStore) ListProviderSummariesByProxyID(ctx context.Context, proxyID int64) ([]egress.ProxyProviderSummary, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, name, platform, type, notes
		FROM providers
		WHERE proxy_id = $1 AND deleted_at IS NULL
		ORDER BY id DESC
	`, proxyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]egress.ProxyProviderSummary, 0)
	for rows.Next() {
		var (
			id       int64
			name     string
			platform string
			accType  string
			notes    sql.NullString
		)
		if err := rows.Scan(&id, &name, &platform, &accType, &notes); err != nil {
			return nil, err
		}
		var notesPtr *string
		if notes.Valid {
			notesPtr = &notes.String
		}
		out = append(out, egress.ProxyProviderSummary{
			ID:       id,
			Name:     name,
			Platform: platform,
			Type:     accType,
			Notes:    notesPtr,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProviderCountsForProxies returns a map of proxy ID to provider count for all proxies.
func (r *ProxyStore) GetProviderCountsForProxies(ctx context.Context) (counts map[int64]int64, err error) {
	rows, err := r.sql.QueryContext(ctx, "SELECT proxy_id, COUNT(*) AS count FROM providers WHERE proxy_id IS NOT NULL AND deleted_at IS NULL GROUP BY proxy_id")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			counts = nil
		}
	}()

	counts = make(map[int64]int64)
	for rows.Next() {
		var proxyID, count int64
		if err = rows.Scan(&proxyID, &count); err != nil {
			return nil, err
		}
		counts[proxyID] = count
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// ListActiveWithProviderCount returns all active proxies with provider count, sorted by creation time descending.
func (r *ProxyStore) ListActiveWithProviderCount(ctx context.Context) ([]egress.ProxyWithProviderCount, error) {
	proxies, err := r.client.Proxy.Query().
		Where(proxy.StatusEQ(egress.StatusActive)).
		Order(dbent.Desc(proxy.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	// Get provider counts
	counts, err := r.GetProviderCountsForProxies(ctx)
	if err != nil {
		return nil, err
	}

	// Build result with provider counts
	result := make([]egress.ProxyWithProviderCount, 0, len(proxies))
	for i := range proxies {
		proxyOut := ProxyEntity(proxies[i])
		if proxyOut == nil {
			continue
		}
		result = append(result, egress.ProxyWithProviderCount{
			Proxy:         *proxyOut,
			ProviderCount: counts[proxyOut.ID],
		})
	}

	return result, nil
}

func ProxyEntity(m *dbent.Proxy) *egress.Proxy {
	if m == nil {
		return nil
	}
	out := &egress.Proxy{
		ID:             m.ID,
		Name:           m.Name,
		Protocol:       m.Protocol,
		Host:           m.Host,
		Port:           m.Port,
		Status:         m.Status,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		ExpiresAt:      m.ExpiresAt,
		FallbackMode:   m.FallbackMode,
		BackupProxyID:  m.BackupProxyID,
		ExpiryWarnDays: m.ExpiryWarnDays,
	}
	if m.Username != nil {
		out.Username = *m.Username
	}
	if m.Password != nil {
		out.Password = *m.Password
	}
	return out
}

func applyProxyEntityToService(dst *egress.Proxy, src *dbent.Proxy) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

// ListAllForFallback 返回所有代理（含过期/非活跃），供改投逻辑使用。
func (r *ProxyStore) ListAllForFallback(ctx context.Context) ([]egress.Proxy, error) {
	proxies, err := r.client.Proxy.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]egress.Proxy, 0, len(proxies))
	for i := range proxies {
		out = append(out, *ProxyEntity(proxies[i]))
	}
	return out, nil
}

// SweepExpiredProxies 将到期的 active 代理标记为 expired，并按 fallback 策略更新提供商 proxy_id，返回受影响的提供商数。
// 每个代理的标记和提供商更新由 sweepOneExpiredProxy 在单独的事务中提交。
// 全部处理后通过 r.sql 发布一次 provider_bulk_changed 事件，使调度缓存失效。
// 事件在这些事务之外发布，失败时记录日志，缓存由调度器周期性全量重建更新。
func (r *ProxyStore) SweepExpiredProxies(ctx context.Context, now time.Time) (int64, error) {
	// 快照读（事务前）：允许脏读不影响正确性，事务内已加锁写。
	all, err := r.ListAllForFallback(ctx)
	if err != nil {
		return 0, err
	}
	byID := make(map[int64]egress.Proxy, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}

	var totalChanged int64
	allChangedProviderIDs := make([]int64, 0)

	for _, p := range all {
		if p.Status != egress.StatusActive || !p.IsExpired(now) {
			continue
		}

		target, change := egress.ResolveProxyFallbackTarget(p, byID, now)
		if !change && p.FallbackMode == egress.FallbackModeProxy {
			// 配置了 proxy 回退但链路无解（成环或全部已过期），记录告警日志
			logger.LegacyPrintf("repository.proxy", "[ProxyExpiry] proxy %d expired but fallback chain unresolved (cycle/all-expired); providers kept", p.ID)
		}

		changedProviderIDs, sweepErr := r.sweepOneExpiredProxy(ctx, p.ID, target, change)
		if sweepErr != nil {
			return totalChanged, sweepErr
		}
		totalChanged += int64(len(changedProviderIDs))
		allChangedProviderIDs = append(allChangedProviderIDs, changedProviderIDs...)
	}

	changedProviderIDs := SortedUniqueProviderIDs(allChangedProviderIDs)
	if len(changedProviderIDs) > 0 {
		// 各代理的改投事务已经提交；这里仅汇总真实被 UPDATE 命中的提供商，
		// 避免代理到期时用全量重建刷新所有调度分桶。
		payload := map[string]any{"provider_ids": changedProviderIDs}
		if err := r.changes.Enqueue(ctx, r.sql, payload); err != nil {
			logger.LegacyPrintf("repository.proxy", "[SchedulerOutbox] enqueue proxy expiry provider changes failed: err=%v", err)
		}
	}
	return totalChanged, nil
}

func SortedUniqueProviderIDs(providerIDs []int64) []int64 {
	if len(providerIDs) < 2 {
		return providerIDs
	}
	slices.Sort(providerIDs)
	write := 1
	for _, providerID := range providerIDs[1:] {
		if providerID == providerIDs[write-1] {
			continue
		}
		providerIDs[write] = providerID
		write++
	}
	return providerIDs[:write]
}

// sweepOneExpiredProxy 在单事务内原子执行：标记代理 expired + 改投绑定提供商。
// 若 r.client 已绑定事务（测试注入场景），直接在 r.sql 上执行，由外层事务保证原子性。
func (r *ProxyStore) sweepOneExpiredProxy(ctx context.Context, proxyID int64, target *int64, change bool) ([]int64, error) {
	// 尝试开启子事务；若 r.client 已是事务 client，则返回 ErrTxStarted，退回使用 r.sql。
	tx, txErr := r.client.Tx(ctx)
	if txErr != nil {
		if txErr != dbent.ErrTxStarted {
			return nil, txErr
		}
		// 已在外层事务中（集成测试场景），直接用 r.sql 执行
		return r.sweepOneExpiredProxyOnExec(ctx, r.sql, proxyID, target, change)
	}

	// 使用新事务执行
	var providerIDs []int64
	var err error
	providerIDs, err = r.sweepOneExpiredProxyOnExec(ctx, tx, proxyID, target, change)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return nil, commitErr
	}
	return providerIDs, nil
}

// sweepOneExpiredProxyOnExec 在给定的 postgresinfra.Executor 上执行：标记 expired + 改投提供商。
func (r *ProxyStore) sweepOneExpiredProxyOnExec(ctx context.Context, exec postgresinfra.Executor, proxyID int64, target *int64, change bool) ([]int64, error) {
	if _, err := exec.ExecContext(ctx,
		`UPDATE proxies SET status=$1, updated_at=NOW() WHERE id=$2 AND deleted_at IS NULL`,
		egress.StatusExpired, proxyID); err != nil {
		return nil, err
	}
	if !change {
		providerIDs, err := r.changes.Providers(exec).InvalidateSnapshots(ctx, proxyID)
		if err != nil {
			return nil, err
		}
		if err := r.EnqueueProviderChanges(ctx, exec, providerIDs); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return r.changes.Providers(exec).Reassign(ctx, proxyID, target)
}

// CountExpired 返回已过期（status=expired）的代理数量。
func (r *ProxyStore) CountExpired(ctx context.Context) (int64, error) {
	var c int64
	err := postgresinfra.ScanSingleRow(ctx, r.sql, `SELECT COUNT(*) FROM proxies WHERE status=$1 AND deleted_at IS NULL`, []any{egress.StatusExpired}, &c)
	return c, err
}

// CountExpiringSoon 返回即将到期（在 expiry_warn_days 天内）的活跃代理数量。
func (r *ProxyStore) CountExpiringSoon(ctx context.Context, now time.Time) (int64, error) {
	var c int64
	err := postgresinfra.ScanSingleRow(ctx, r.sql, `
		SELECT COUNT(*) FROM proxies
		WHERE deleted_at IS NULL AND status=$1 AND expires_at IS NOT NULL
		  AND expires_at > $2 AND expires_at <= $2 + (expiry_warn_days || ' days')::interval`,
		[]any{egress.StatusActive, now}, &c)
	return c, err
}
