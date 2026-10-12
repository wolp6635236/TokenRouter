package ws

import (
	"errors"
	"sync"

	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// OpenAIWSConnections 持有唯一按需连接池与共享拨号器，关闭后不能重新建池。
type OpenAIWSConnections struct {
	Options    *WSPoolOptions
	dialer     openai.WSClientDialer
	dialerOnce sync.Once
	pool       *WSConnPool
	poolOnce   sync.Once
	poolMu     sync.Mutex
	// updateMu 串行执行参数更新和关闭，poolMu 保护连接池的创建与引用。
	updateMu sync.Mutex
	closed   bool
}

// NewOpenAIWSConnections 保存配置和拨号器，连接池在首次使用时启动。
func NewOpenAIWSConnections(options *WSPoolOptions, dialer openai.WSClientDialer, pools ...*WSConnPool) *OpenAIWSConnections {
	value := &OpenAIWSConnections{Options: options, dialer: dialer}
	if len(pools) > 0 {
		value.pool = pools[0]
	}
	return value
}

func (s *OpenAIWSConnections) Pool() *WSConnPool {
	if s == nil {
		return nil
	}
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	if s.closed {
		return s.pool
	}
	s.poolOnce.Do(func() {
		if s.pool == nil {
			s.pool = NewWSConnPool(s.Options)
			s.pool.Start()
		}
	})
	return s.pool
}

func (s *OpenAIWSConnections) Dialer() openai.WSClientDialer {
	if s == nil {
		return nil
	}
	s.dialerOnce.Do(func() {
		if s.dialer == nil {
			s.dialer = openai.NewDefaultWSClientDialer()
		}
	})
	return s.dialer
}

func (s *OpenAIWSConnections) InvalidateProvider(providerID int64) {
	if pool := s.Pool(); pool != nil {
		pool.ClearProvider(providerID)
	}
}

// Close 关闭连接池并标记停止，后续 Pool 调用返回已关闭的池。
func (s *OpenAIWSConnections) Close() {
	if s == nil {
		return
	}
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	s.poolMu.Lock()
	s.closed = true
	pool := s.pool
	s.poolMu.Unlock()
	if pool != nil {
		pool.Close()
	}
}

// UpdateOptions 串行发布参数，回收连接时其他请求仍可取得池引用。
func (s *OpenAIWSConnections) UpdateOptions(options WSPoolOptions) error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	s.poolMu.Lock()
	if s.closed {
		s.poolMu.Unlock()
		return errors.New("responses websocket connections are stopped")
	}
	s.Options = &options
	pool := s.pool
	s.poolMu.Unlock()
	if pool != nil {
		return pool.UpdateOptions(options)
	}
	return nil
}
