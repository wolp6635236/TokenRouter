package provider_test

import (
	"context"
	"errors"
	"log/slog"
	"time"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

// credentialReadStore 提供凭据测试需要的提供商读取方法。
type credentialReadStore struct {
	gatewayprovider.ExecutionProviderStore
	providersByID map[int64]*gatewayprovider.ExecutionProvider
}

func (s *credentialReadStore) GetByID(_ context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	if value, ok := s.providersByID[id]; ok {
		return value, nil
	}
	return nil, errors.New("provider not found")
}

func grokCredentialMutationSnapshot(value *gatewayprovider.ExecutionProvider) providercore.CredentialMutationSnapshot {
	return providercore.GrokCredentialMutationSnapshot(gatewayprovider.ExecutionRecord(value))
}

func credentialMutationForTest(value forwardcore.GrokCredentialFailure) providercore.GrokCredentialMutation {
	return providercore.GrokCredentialMutation{Permanent: value.Permanent, Transient: value.Transient, Reason: string(value.Reason), Snapshot: value.Snapshot(), VerifyMissing: value.Reason == forwardcore.GrokCredentialReasonMissing, VerifyProxy: value.Reason == forwardcore.GrokCredentialReasonProxyInvalid}
}

func credentialBlocked(value *gatewayhttp.RequestCredentialExecutor, target *gatewayprovider.ExecutionProvider) bool {
	return value.Runtime.Runtime.Blocked(target.Record.ID, func() string { return providercore.RefreshCredentialIdentity(gatewayprovider.ExecutionRecord(target)) })
}

func newRequestCredentialsFixture(store gatewayprovider.ExecutionProviderStore, tokens *providercore.GrokTokenSource) *gatewayhttp.RequestCredentialExecutor {
	blocks := providercore.NewRuntimeBlockState(time.Now)
	recovery := &providercore.GrokCredentialRecovery{Runtime: blocks, Warn: slog.Warn}
	source := &providercore.OpenAIExecutionCredentials{}
	if store != nil {
		source.Parent = func(ctx context.Context, id int64) (*providercore.Record, error) {
			value, err := store.GetByID(ctx, id)
			return gatewayprovider.ExecutionRecord(value), err
		}
		recovery.Read = func(ctx context.Context, id int64) (*providercore.Record, error) {
			if held, ok := ctx.Value(credentialMutationHoldKey{}).(*credentialMutationHold); ok {
				return held.read(ctx)
			}
			return source.Parent(ctx, id)
		}
		recovery.State, _ = store.(providercore.GrokCredentialStateWriter)
	}
	if tokens != nil {
		source.Grok = tokens.GetAccessToken
		recovery.Invalidate = tokens.InvalidateToken
	}
	return &gatewayhttp.RequestCredentialExecutor{Runtime: &gatewayprovider.RequestCredentials{Source: source, HasGrokTokenSource: tokens != nil, Recovery: recovery, Runtime: blocks}}
}

// 占锁夹具调用 Apply，并在读取阶段阻塞。
// 取消占锁操作后等待它退出，不执行任何条件写入。
type (
	credentialMutationHoldKey struct{}
	credentialMutationHold    struct {
		core    *providercore.GrokCredentialRecovery
		id      int64
		started chan struct{}
		done    chan struct{}
		cancel  context.CancelFunc
		restore func()
	}
)

func newCredentialMutationHold(core *providercore.GrokCredentialRecovery, id int64) *credentialMutationHold {
	return &credentialMutationHold{core: core, id: id, started: make(chan struct{}), done: make(chan struct{})}
}

func (h *credentialMutationHold) read(ctx context.Context) (*providercore.Record, error) {
	close(h.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (h *credentialMutationHold) Lock(ctx context.Context) error {
	if h.core.Read == nil {
		h.core.Read = func(ctx context.Context, _ int64) (*providercore.Record, error) { return h.read(ctx) }
		h.restore = func() { h.core.Read = nil }
	}
	run, cancel := context.WithCancel(context.WithValue(ctx, credentialMutationHoldKey{}, h))
	h.cancel = cancel
	go func() {
		defer close(h.done)
		_, _ = h.core.Apply(run, &providercore.Record{ID: h.id}, providercore.GrokCredentialMutation{Transient: true})
	}()
	select {
	case <-h.started:
		return nil
	case <-ctx.Done():
		cancel()
		<-h.done
		return ctx.Err()
	}
}

func (h *credentialMutationHold) Unlock() {
	h.cancel()
	<-h.done
	if h.restore != nil {
		h.restore()
	}
}
