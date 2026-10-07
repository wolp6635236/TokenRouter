package settings

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

var ErrSettingNotFound = apperror.NotFound("SETTING_NOT_FOUND", "setting not found")

// Setting 是原 settings 表的稳定值类型。
type Setting struct {
	ID        int64
	Key       string
	Value     string
	UpdatedAt time.Time
}

// Repository 仅负责设置读写；批量写入必须保持单次原子提交。
type Repository interface {
	Get(context.Context, string) (*Setting, error)
	GetValue(context.Context, string) (string, error)
	Set(context.Context, string, string) error
	GetMultiple(context.Context, []string) (map[string]string, error)
	SetMultiple(context.Context, map[string]string) error
	GetAll(context.Context) (map[string]string, error)
	Delete(context.Context, string) error
}

// ConditionalRepository 在一次事务中比较旧值并提交整个设置批次。
type ConditionalRepository interface {
	Repository
	CompareAndSetMultiple(context.Context, map[string]string, map[string]*string) error
}

type subscription struct {
	active atomic.Bool
	id     uint64
	fn     func()
}

// Store 提供设置读写和订阅通知，业务校验和领域缓存在调用模块处理。
// 调用方在设置持久化和缓存刷新完成后调用 NotifyUpdated。
// @project-doc docs/interfaces/configuration.md#runtime_settings
type Store struct {
	updates     *Updates
	repo        Repository
	mu          sync.RWMutex
	version     string
	nextID      uint64
	subscribers []*subscription
}

// New 在 repo 为 Store 时返回该实例，否则为 repo 创建 Store。
func New(repo Repository) *Store {
	if store, ok := repo.(*Store); ok {
		return store
	}
	return &Store{repo: repo, updates: newUpdates(repo)}
}

func (s *Store) Get(ctx context.Context, key string) (*Setting, error) { return s.repo.Get(ctx, key) }

func (s *Store) GetValue(ctx context.Context, key string) (string, error) {
	return s.repo.GetValue(ctx, key)
}

func (s *Store) Set(ctx context.Context, key, value string) error { return s.repo.Set(ctx, key, value) }

func (s *Store) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	return s.repo.GetMultiple(ctx, keys)
}

func (s *Store) SetMultiple(ctx context.Context, values map[string]string) error {
	return s.repo.SetMultiple(ctx, values)
}
func (s *Store) GetAll(ctx context.Context) (map[string]string, error) { return s.repo.GetAll(ctx) }
func (s *Store) Delete(ctx context.Context, key string) error          { return s.repo.Delete(ctx, key) }

// SetVersion 设置应用版本。
func (s *Store) SetVersion(version string) {
	s.mu.Lock()
	s.version = version
	s.mu.Unlock()
}

func (s *Store) Version() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// Subscribe 按登记顺序同步通知。注销幂等并阻止尚未领取的回调；已领取的回调可以完成一次。
func (s *Store) Subscribe(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	sub := &subscription{id: id, fn: fn}
	sub.active.Store(true)
	s.subscribers = append(s.subscribers, sub)
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			for i, sub := range s.subscribers {
				if sub.id == id {
					sub.active.Store(false)
					s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
					return
				}
			}
		})
	}
}

// NotifyUpdated 通知已登记的订阅者，调用方在持久化和缓存刷新后调用。
// 回调在锁外执行，允许回调注销自身或继续读取设置。
func (s *Store) NotifyUpdated() {
	s.mu.RLock()
	subs := append([]*subscription(nil), s.subscribers...)
	s.mu.RUnlock()
	for _, sub := range subs {
		if sub.active.Load() {
			sub.fn()
		}
	}
}

// Updates 返回与 Store 共用生命周期的综合更新协调器。
func (s *Store) Updates() *Updates { return s.updates }
