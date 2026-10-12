package requestlog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
)

const (
	requestBufferCapacity = 4096
	requestBatchSize      = 256
	requestFlushInterval  = 20 * time.Millisecond
	requestRetryInterval  = time.Second
	requestWriteTimeout   = 5 * time.Second
)

// ErrInvalidRecord 表示快照内容无法存储，重复写入同一版本仍会失败。
var ErrInvalidRecord = errors.New("invalid request record")

// Repository 保存请求摘要，并按调用者权限查询现存记录。
type Repository interface {
	Save(context.Context, []telemetry.RequestRecord) error
	Find(context.Context, string, int64, bool) ([]Detail, error)
	Cleanup(context.Context, time.Time) error
}

// Usage 是请求详情中的费用和用量摘要。
type Usage struct {
	ID           int64   `json:"id"`
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	ActualCost   float64 `json:"actual_cost"`
}

// Failure 是经过归属检查的错误引用。
type Failure struct {
	ID     int64  `json:"id"`
	Status int    `json:"status_code"`
	Phase  string `json:"phase"`
}

// Detail 汇总请求摘要及仍在保留期内的业务记录。
type Detail struct {
	telemetry.RequestRecord
	Usage    []Usage   `json:"usage"`
	Errors   []Failure `json:"errors"`
	AuditIDs []int64   `json:"audit_ids,omitempty"`
	Legacy   bool      `json:"legacy"`
	Pending  bool      `json:"pending,omitempty"`
}

// Health 表示待写记录和累计写入故障。
type Health struct {
	Pending  int    `json:"pending"`
	Failures uint64 `json:"failures"`
}

// Service 在内存中合并请求快照，由后台按批写入数据库。
type Service struct {
	repo      Repository
	retention time.Duration
	logf      func(string, ...any)
	failures  atomic.Uint64

	mu      sync.Mutex
	pending map[string]telemetry.RequestRecord
	stopped bool
	writes  sync.WaitGroup
	flushMu sync.Mutex

	ctx       context.Context
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
	done      chan struct{}
	stopDone  chan struct{}
	stopErr   error
}

// NewService 配置请求摘要的批写和留存。
// @project-doc docs/operations/request_lookup.md#request_storage
func NewService(repo Repository, retentionDays int, logf func(string, ...any)) *Service {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		repo: repo, retention: time.Duration(retentionDays) * 24 * time.Hour, logf: logf,
		pending: make(map[string]telemetry.RequestRecord), ctx: ctx, cancel: cancel,
		done: make(chan struct{}), stopDone: make(chan struct{}),
	}
}

// Observe 合并同一请求的待写快照，缓冲已满或服务停止时同步写库。
func (s *Service) Observe(record telemetry.RequestRecord) {
	if s == nil || s.repo == nil {
		return
	}
	if record.RequestID == "" {
		s.report("write request record", fmt.Errorf("missing request ID"))
		return
	}
	record = telemetry.NormalizeRequestRecord(record)
	s.mu.Lock()
	if !s.stopped {
		if previous, ok := s.pending[record.RequestID]; ok {
			s.pending[record.RequestID] = telemetry.MergeRequestRecord(previous, record)
			s.mu.Unlock()
			return
		}
		if len(s.pending) < requestBufferCapacity {
			s.pending[record.RequestID] = record
			s.mu.Unlock()
			return
		}
		// 关闭入队后等待已经开始的同步写入，停止后的调用自行完成写库。
		s.writes.Add(1)
		defer s.writes.Done()
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), requestWriteTimeout)
	defer cancel()
	if err := s.repo.Save(ctx, []telemetry.RequestRecord{record}); err != nil {
		s.report("write request "+record.RequestID, err)
	}
}

func (s *Service) Start(context.Context) error {
	s.startOnce.Do(func() { go s.run() })
	return nil
}

func (s *Service) run() {
	defer close(s.done)
	ticker := time.NewTicker(requestFlushInterval)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(s.ctx, requestWriteTimeout)
			if err := s.Flush(ctx); err != nil {
				if s.ctx.Err() == nil {
					s.report("flush request records", err)
				}
				// 持续失败时降低重试频率，数据库恢复后再按批写窗口调度。
				ticker.Reset(requestRetryInterval)
			} else {
				ticker.Reset(requestFlushInterval)
			}
			cancel()
		case <-cleanup.C:
			ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
			if err := s.repo.Cleanup(ctx, time.Now().Add(-s.retention)); err != nil && s.ctx.Err() == nil {
				s.report("clean request records", err)
			}
			cancel()
		}
	}
}

// Stop 关闭内存入队并排空，等待时间由应用的关闭期限控制。
func (s *Service) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		s.cancel()
		s.startOnce.Do(func() { close(s.done) })
		go func() {
			defer close(s.stopDone)
			<-s.done
			s.writes.Wait()
			s.stopErr = s.drain(ctx)
		}()
	})
	select {
	case <-s.stopDone:
		return s.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) drain(ctx context.Context) error {
	for {
		health, _ := s.Health()
		if health.Pending == 0 {
			return nil
		}
		if err := s.Flush(ctx); err != nil {
			s.report("drain request records", err)
			return err
		}
	}
}

// Flush 批量写入快照，失败时在同一超时窗口内尝试逐条写入。
func (s *Service) Flush(ctx context.Context) error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	batch := make([]telemetry.RequestRecord, 0, min(len(s.pending), requestBatchSize))
	for _, record := range s.pending {
		batch = append(batch, record)
		if len(batch) == requestBatchSize {
			break
		}
	}
	s.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, requestWriteTimeout)
	defer cancel()
	if err := s.repo.Save(writeCtx, batch); err != nil {
		if len(batch) == 1 {
			if s.discardInvalid(batch[0], err) {
				return nil
			}
			return err
		}
		if writeCtx.Err() != nil {
			return err
		}
		s.report("batch write request records", err)
		var failures []error
		for _, record := range batch {
			if err := writeCtx.Err(); err != nil {
				return errors.Join(append(failures, err)...)
			}
			if err := s.repo.Save(writeCtx, []telemetry.RequestRecord{record}); err != nil {
				if !s.discardInvalid(record, err) {
					failures = append(failures, fmt.Errorf("request %s: %w", record.RequestID, err))
				}
			} else {
				s.ack(record)
			}
		}
		return errors.Join(failures...)
	}
	for _, record := range batch {
		s.ack(record)
	}
	return nil
}

// discardInvalid 移除已确认无效的版本，并通过日志和故障计数报告丢弃。
func (s *Service) discardInvalid(record telemetry.RequestRecord, err error) bool {
	if !errors.Is(err, ErrInvalidRecord) {
		return false
	}
	s.ack(record)
	s.report("discard invalid request "+record.RequestID, err)
	return true
}

// ack 移除指定版本，写库期间收到的更新继续等待下一批。
func (s *Service) ack(record telemetry.RequestRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.pending[record.RequestID]; ok && current.UpdatedAt.Equal(record.UpdatedAt) {
		delete(s.pending, record.RequestID)
	}
}

func (s *Service) Find(ctx context.Context, id string, userID int64, admin bool) ([]Detail, error) {
	items, err := s.repo.Find(ctx, id, userID, admin)
	s.mu.Lock()
	record, found := s.pending[id]
	s.mu.Unlock()
	if found && (admin || record.UserID > 0 && record.UserID == userID) {
		record = telemetry.CloneRequestRecord(record)
		for i := range items {
			if items[i].RequestID == id {
				items[i].RequestRecord = telemetry.MergeRequestRecord(items[i].RequestRecord, record)
				items[i].Pending = true
				return items, nil
			}
		}
		return append(items, Detail{RequestRecord: record, Pending: true}), nil
	}
	return items, err
}

func (s *Service) Health() (Health, error) {
	s.mu.Lock()
	pending := len(s.pending)
	s.mu.Unlock()
	return Health{Pending: pending, Failures: s.failures.Load()}, nil
}

func (s *Service) report(action string, err error) {
	s.failures.Add(1)
	if s.logf != nil {
		s.logf("%s: %v", action, fmt.Errorf("request records: %w", err))
	}
}
