package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	wire "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

// Created 保存公共响应和选中提供商的标识。
type Created struct {
	SDP        []byte
	CallID     string
	Location   string
	ProviderID int64
}

// CreateTarget 提供本次选择的模型数据、客户端资格和供应商请求操作。
type CreateTarget interface {
	ResolveModel(context.Context, string) (string, string, error)
	AllowsClient(context.Context, session.LiveCallIdentity) bool
	Create(context.Context, *session.LiveCallRequest, string) (*Created, error)
}

// Candidate 拥有当前提供商槽的释放能力，Live 租约接替后立即释放普通槽。
type Candidate struct {
	ID          int64
	Concurrency int
	Acquired    bool
	ReleaseFunc func()
	Target      CreateTarget
}

// CreatePorts 提供凭据、选路和观测操作。
type CreatePorts interface {
	PrepareAttestation(context.Context) (string, string, error)
	Select(context.Context, *int64, string, map[int64]struct{}) (*Candidate, error)
	TraceModels(context.Context, string, string)
	ModelTrace(context.Context, *int64, string, string) (string, string)
	NewLeaseID() string
	ShouldFailover(error) bool
	Observe(*session.LiveCallRecord)
}

// Creator 负责 Live 创建尝试、租约接替、会话保存和 observer 启动顺序。
type Creator struct {
	runtime     *Service
	ports       CreatePorts
	maxDuration time.Duration
}

func NewCreator(runtime *Service, ports CreatePorts, maxDuration time.Duration) *Creator {
	return &Creator{runtime: runtime, ports: ports, maxDuration: maxDuration}
}

func HashCallID(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return hex.EncodeToString(sum[:])
}

func liveGroupID(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

// Create 创建 Frameless 会话。调用方须在调用期间持有普通用户槽位；
// 调度器持有的普通提供商槽位会被同一个 Live 租约原子接替。
func (s *Creator) Create(
	ctx context.Context,
	request *session.LiveCallRequest,
	identity session.LiveCallIdentity,
	userMaxConcurrency int,
) (*Created, error) {
	if err := wire.ValidateLiveCallRequest(request); err != nil {
		return nil, err
	}
	store, err := s.runtime.ports.Store()
	if err != nil {
		return nil, err
	}
	liveCache, err := s.runtime.ports.Leases()
	if err != nil {
		return nil, err
	}
	attestation, attestationCiphertext, err := s.ports.PrepareAttestation(ctx)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(gjson.GetBytes(request.Session, "model").String())
	if model == "" {
		model = "gpt-live"
	}

	excluded := make(map[int64]struct{})
	var lastErr error
	for range 4 {
		selection, selectErr := s.ports.Select(ctx, identity.GroupID, model, excluded)

		if selectErr != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, selectErr
		}
		if selection == nil || selection.Target == nil || !selection.Acquired {
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			return nil, session.ErrLiveConcurrencyFull
		}

		provider := selection
		routingModel, upstreamModel, routingErr := provider.Target.ResolveModel(ctx, model)

		if routingErr != nil {
			selection.ReleaseFunc()
			excluded[provider.ID] = struct{}{}
			lastErr = routingErr
			continue
		}
		if strings.TrimSpace(upstreamModel) == "" {
			upstreamModel = routingModel
		}
		s.ports.TraceModels(ctx, routingModel, upstreamModel)
		upstreamSession, rewriteErr := sjson.SetBytes(request.Session, "model", upstreamModel)
		if rewriteErr != nil {
			selection.ReleaseFunc()
			return nil, rewriteErr
		}
		upstreamRequest := &session.LiveCallRequest{SDP: request.SDP, Session: upstreamSession}
		if !provider.Target.AllowsClient(ctx, identity) {
			selection.ReleaseFunc()
			excluded[provider.ID] = struct{}{}
			lastErr = session.ErrLiveClientPolicyDenied
			continue
		}
		leaseID := s.ports.NewLeaseID()
		acquired, acquireErr := liveCache.AcquireLiveLease(
			ctx,
			provider.ID,
			provider.Concurrency,
			identity.UserID,
			userMaxConcurrency,
			identity.APIKeyID,
			leaseID,
			true,
		)
		if acquireErr != nil || !acquired {
			selection.ReleaseFunc()
			if acquireErr != nil {
				return nil, acquireErr
			}
			return nil, session.ErrLiveConcurrencyFull
		}

		created, createErr := provider.Target.Create(ctx, upstreamRequest, attestation)
		selection.ReleaseFunc()
		if createErr != nil {
			s.runtime.ReleaseLease(provider.ID, identity.UserID, identity.APIKeyID, leaseID)
			if !s.ports.ShouldFailover(createErr) {
				return nil, createErr
			}
			excluded[provider.ID] = struct{}{}
			lastErr = createErr
			continue
		}

		now := time.Now()
		requestedModel, mappingChain := s.ports.ModelTrace(ctx, identity.GroupID, model, upstreamModel)
		record := &session.LiveCallRecord{
			CallID:                created.CallID,
			CallHash:              HashCallID(created.CallID),
			ProviderID:            provider.ID,
			APIKeyID:              identity.APIKeyID,
			ActorUserID:           identity.ActorUserID,
			UserID:                identity.UserID,
			TeamID:                liveGroupID(identity.TeamID),
			GroupID:               liveGroupID(identity.GroupID),
			SubscriptionID:        liveGroupID(identity.SubscriptionID),
			LeaseID:               leaseID,
			Model:                 model,
			RequestedModel:        requestedModel,
			UpstreamModel:         upstreamModel,
			ModelMappingChain:     mappingChain,
			APIKeyModelMapping:    cloneModelMapping(identity.ModelMapping),
			CreatedAt:             now,
			ExpiresAt:             now.Add(s.maxDuration),
			Controller:            session.LiveControllerPending,
			UserAgent:             identity.UserAgent,
			IPAddress:             identity.IPAddress,
			InboundEndpoint:       identity.InboundEndpoint,
			AttestationCiphertext: attestationCiphertext,
		}
		mappingTTL := s.maxDuration + 5*time.Minute
		if saveErr := store.SaveLiveCall(ctx, record, mappingTTL); saveErr != nil {
			s.runtime.ReleaseLease(provider.ID, identity.UserID, identity.APIKeyID, leaseID)
			return nil, fmt.Errorf("save live call mapping: %w", saveErr)
		}
		telemetry.RecordRelatedRequest(ctx, telemetry.RequestRecord{RequestID: telemetry.StableRequestID("live/" + record.CallHash), State: "running", StartedAt: now, UserID: record.ActorUserID, APIKeyID: record.APIKeyID, ProviderID: record.ProviderID, Model: record.RequestedModel, Aliases: []telemetry.RequestAlias{{Kind: "billing", Value: record.CallHash}}})
		created.ProviderID = provider.ID
		s.ports.Observe(record)
		return created, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, session.ErrLiveUnavailable
}

// cloneModelMapping 复制模型映射，nil 输入转换为空对象。
func cloneModelMapping(mapping map[string]string) map[string]string {
	result := make(map[string]string, len(mapping))
	maps.Copy(result, mapping)
	return result
}
