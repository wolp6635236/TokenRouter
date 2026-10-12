package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const DefaultQoderSessionBuildTimeout = 90 * time.Second

var (
	ErrQoderSessionBuildInvalidated = errors.New("qoder session invalidated during build")
	ErrQoderSessionsStopped         = errors.New("qoder credential sessions are stopped")
)

type QoderSessions[T comparable] struct {
	Mu                  sync.Mutex
	Sessions            map[int64]QoderSessionCacheEntry[T]
	ProviderStates      map[int64]QoderSessionProviderState
	SessionBuildGroup   singleflight.Group
	SessionBuildTimeout time.Duration
	activity            operationActivity
}

type QoderSessionCacheEntry[T comparable] struct {
	CredentialsHash string
	Session         T
	ExpiresAt       time.Time
}

// QoderSessionProviderState 保存单个提供商已观察到的最新凭据顺序与异步构建快照。
type QoderSessionProviderState struct {
	CredentialsHash   string
	CredentialVersion int64
	ProviderSnapshot  *Record
	Generation        uint64
}

// qoderSessionBuildResult[T] 保存 singleflight 共享的 session 构建结果。
type qoderSessionBuildResult[T comparable] struct {
	Session T
}

// StopContext 取消共享构建并等待已进入工作，停止后禁止新构建及迟到回填。
func (p *QoderSessions[T]) StopContext(ctx context.Context) error {
	if p == nil {
		return nil
	}
	return p.activity.stop(ctx, "qoder credentials")
}

// @project-doc docs/interfaces/qoder_upstream.md#qoder_account_contract
func (p *QoderSessions[T]) GetSession(ctx context.Context, provider *Record, build func(context.Context, *Record) (T, time.Time, error)) (T, error) {
	var zero T
	if p == nil {
		return zero, errors.New("qoder token provider is nil")
	}
	if provider == nil {
		return zero, errors.New("provider is nil")
	}
	if provider.Platform != PlatformQoder || provider.Type != ProviderTypeCosy {
		return zero, errors.New("not a qoder cosy provider")
	}

	operation, finish, beginErr := p.activity.begin(context.WithoutCancel(ctx), ErrQoderSessionsStopped)
	if beginErr != nil {
		return zero, beginErr
	}
	defer finish()
	providerSnapshot := CloneRecord(provider)
	hash := QoderCredentialsHash(providerSnapshot.Credentials)
	generation, hash, providerSnapshot, session := p.prepareQoderSessionBuild(providerSnapshot, hash)
	if session != zero {
		return session, nil
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	// 凭据哈希与失效世代共同组成 key：相同请求只回源一次，失效或改密后不会等待旧交换。
	flightKey := fmt.Sprintf("%d:%s:%d", providerSnapshot.ID, hash, generation)
	resultCh := p.SessionBuildGroup.DoChan(flightKey, func() (any, error) {
		// 共享构建不能绑定首个等待者；每个调用者在下方独立响应自己的取消信号。
		buildTimeout := p.SessionBuildTimeout
		if buildTimeout <= 0 {
			buildTimeout = DefaultQoderSessionBuildTimeout
		}
		managed, finishBuild, beginErr := p.activity.begin(context.WithoutCancel(ctx), ErrQoderSessionsStopped)
		if beginErr != nil {
			return zero, beginErr
		}
		defer finishBuild()
		buildCtx, cancel := context.WithTimeout(managed, buildTimeout)
		defer cancel()

		if cached, current := p.cachedQoderSessionForBuild(providerSnapshot.ID, hash, generation); !current {
			return zero, ErrQoderSessionBuildInvalidated
		} else if cached != zero {
			return &qoderSessionBuildResult[T]{Session: cached}, nil
		}

		session, expiresAt, buildErr := build(buildCtx, providerSnapshot)
		if buildErr != nil {
			return zero, buildErr
		}
		if err := buildCtx.Err(); err != nil {
			return zero, err
		}
		if !p.storeQoderSessionBuild(providerSnapshot.ID, hash, generation, session, expiresAt) {
			return zero, ErrQoderSessionBuildInvalidated
		}
		return &qoderSessionBuildResult[T]{Session: session}, nil
	})

	var value any
	select {
	case <-operation.Done():
		return zero, ErrQoderSessionsStopped
	case <-ctx.Done():
		return zero, ctx.Err()
	case flightResult := <-resultCh:
		if flightResult.Err != nil {
			// 被失效或改密打断的旧调用不能返回已经失效的 session，由后续业务调用按新凭据重建。
			return zero, flightResult.Err
		}
		value = flightResult.Val
	}
	result, ok := value.(*qoderSessionBuildResult[T])
	if !ok || result == nil || result.Session == zero {
		return zero, errors.New("qoder session build returned an invalid result")
	}
	return result.Session, nil
}

// QoderCredentialSnapshotOlder 比较凭据快照顺序：明确的 token_version 优先，无法判序时才参考更新时间。
func QoderCredentialSnapshotOlder(incoming, current *Record) bool {
	if incoming == nil || current == nil {
		return false
	}
	incomingVersion := incoming.GetCredentialAsInt64("_token_version")
	currentVersion := current.GetCredentialAsInt64("_token_version")
	if incomingVersion > 0 && currentVersion > 0 && incomingVersion != currentVersion {
		return incomingVersion < currentVersion
	}
	if incomingVersion > 0 && currentVersion == 0 {
		return false
	}
	if incomingVersion == 0 && currentVersion > 0 {
		return incoming.UpdatedAt.IsZero() || current.UpdatedAt.IsZero() || !incoming.UpdatedAt.After(current.UpdatedAt)
	}
	return !incoming.UpdatedAt.IsZero() && !current.UpdatedAt.IsZero() && incoming.UpdatedAt.Before(current.UpdatedAt)
}

// prepareQoderSessionBuild 返回当前凭据，调度快照版本较低时使用已观察到的新凭据。
func (p *QoderSessions[T]) prepareQoderSessionBuild(provider *Record, hash string) (uint64, string, *Record, T) {
	var zero T
	p.Mu.Lock()
	defer p.Mu.Unlock()
	providerID := provider.ID
	if p.Sessions == nil {
		p.Sessions = make(map[int64]QoderSessionCacheEntry[T])
	}
	if p.ProviderStates == nil {
		p.ProviderStates = make(map[int64]QoderSessionProviderState)
	}

	incomingVersion := provider.GetCredentialAsInt64("_token_version")
	state := p.ProviderStates[providerID]
	if state.CredentialsHash == "" {
		state.CredentialsHash = hash
		state.CredentialVersion = incomingVersion
		state.ProviderSnapshot = provider
	} else if state.CredentialsHash != hash {
		current := state.ProviderSnapshot
		if QoderCredentialSnapshotOlder(provider, current) {
			// 刷新后的 token_version 单调递增；旧 scheduler 快照改用已观察到的新凭据构建或加入 flight。
			hash = state.CredentialsHash
			provider = current
		} else {
			state.Generation++
			state.CredentialsHash = hash
			state.CredentialVersion = incomingVersion
			state.ProviderSnapshot = provider
			delete(p.Sessions, providerID)
		}
	} else {
		// 相同凭据下保留最新的代理、TLS 等非哈希运行时配置，供后续过期重建使用。
		state.ProviderSnapshot = provider
	}
	p.ProviderStates[providerID] = state
	if entry, ok := p.Sessions[providerID]; ok && entry.CredentialsHash == hash && entry.Session != zero && !qoderSessionCacheEntryExpired(entry, time.Now()) {
		return state.Generation, hash, provider, entry.Session
	}
	delete(p.Sessions, providerID)
	return state.Generation, hash, provider, zero
}

// cachedQoderSessionForBuild 在 singleflight 回调中复查缓存和代次，排队期间已有结果时直接使用。
func (p *QoderSessions[T]) cachedQoderSessionForBuild(providerID int64, hash string, generation uint64) (T, bool) {
	var zero T
	p.Mu.Lock()
	defer p.Mu.Unlock()
	state, ok := p.ProviderStates[providerID]
	if !ok || state.Generation != generation || state.CredentialsHash != hash {
		return zero, false
	}
	entry, ok := p.Sessions[providerID]
	if !ok || entry.CredentialsHash != hash || entry.Session == zero || qoderSessionCacheEntryExpired(entry, time.Now()) {
		delete(p.Sessions, providerID)
		return zero, true
	}
	return entry.Session, true
}

// storeQoderSessionBuild 只允许当前凭据世代写入，防止慢速旧交换覆盖新 session。
func (p *QoderSessions[T]) storeQoderSessionBuild(providerID int64, hash string, generation uint64, session T, expiresAt time.Time) bool {
	p.activity.mu.Lock()
	defer p.activity.mu.Unlock()
	if p.activity.stopped {
		return false
	}
	p.Mu.Lock()
	defer p.Mu.Unlock()
	state, ok := p.ProviderStates[providerID]
	if !ok || state.Generation != generation || state.CredentialsHash != hash {
		return false
	}
	p.Sessions[providerID] = QoderSessionCacheEntry[T]{CredentialsHash: hash, Session: session, ExpiresAt: expiresAt}
	return true
}

// qoderSessionCacheEntryExpired 判断带明确有效期的国内 PAT session 是否已经过期。
func qoderSessionCacheEntryExpired[T comparable](entry QoderSessionCacheEntry[T], now time.Time) bool {
	return !entry.ExpiresAt.IsZero() && !now.Before(entry.ExpiresAt)
}

func (p *QoderSessions[T]) Invalidate(providerID int64) {
	if p == nil {
		return
	}
	p.Mu.Lock()
	if p.ProviderStates == nil {
		p.ProviderStates = make(map[int64]QoderSessionProviderState)
	}
	state := p.ProviderStates[providerID]
	state.Generation++
	p.ProviderStates[providerID] = state
	delete(p.Sessions, providerID)
	p.Mu.Unlock()
}

// InvalidateProvider 根据数据库或刷新结果中的提供商快照使 session 失效，后续请求使用该快照的 token。
func (p *QoderSessions[T]) InvalidateProvider(provider *Record) {
	if p == nil || provider == nil {
		return
	}
	snapshot := CloneRecord(provider)
	hash := QoderCredentialsHash(snapshot.Credentials)

	p.Mu.Lock()
	if p.Sessions == nil {
		p.Sessions = make(map[int64]QoderSessionCacheEntry[T])
	}
	if p.ProviderStates == nil {
		p.ProviderStates = make(map[int64]QoderSessionProviderState)
	}
	state := p.ProviderStates[snapshot.ID]
	incomingVersion := snapshot.GetCredentialAsInt64("_token_version")
	if current := state.ProviderSnapshot; current != nil {
		if QoderCredentialSnapshotOlder(snapshot, current) {
			// 迟到的刷新/DB 快照不能删除已经缓存的更新 session，也不能提升失效世代。
			p.Mu.Unlock()
			return
		}
	}
	state.CredentialsHash = hash
	state.CredentialVersion = incomingVersion
	state.ProviderSnapshot = snapshot
	state.Generation++
	p.ProviderStates[snapshot.ID] = state
	delete(p.Sessions, snapshot.ID)
	p.Mu.Unlock()
}

func QoderCredentialsHash(credentials map[string]any) string {
	body, _ := json.Marshal(credentials)
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}
