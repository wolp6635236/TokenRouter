package selection

import (
	"context"
	"fmt"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// Probe 选择分组提供商并测试，传入本次分组映射后的模型。
type Probe struct {
	ProviderTest    ProviderTester
	gatewaySvc      *Generic
	openAIGateway   *Compatible
	geminiCompatSvc *Gemini
}

// ProviderTester 定义已选提供商的测试操作。
type ProviderTester interface {
	RunTestBackgroundWithPromptAndUserAgent(context.Context, int64, string, string, string) (*provider.ScheduledTestResult, error)
}

// NewProbe 绑定平台选择器和提供商测试器。
func NewProbe(test ProviderTester, generic *Generic, compatible *Compatible, gemini *Gemini) Probe {
	return Probe{ProviderTest: test, gatewaySvc: generic, openAIGateway: compatible, geminiCompatSvc: gemini}
}

func (s Probe) Select(ctx context.Context, due routing.GroupAvailabilityProbeDueGroup, model string) (routing.GroupProbeTarget, error) {
	provider, err := s.selectProbeProvider(ctx, due, model)
	if err != nil {
		return routing.GroupProbeTarget{}, err
	}
	// 选择仍以请求模型校验白名单，测试只接收分组映射结果，避免丢失别名或重复映射。
	routingModel := model
	if s.openAIGateway != nil {
		routingModel = s.openAIGateway.resolveGroupRoutingModel(ctx, &due.GroupID, model)
	} else if s.gatewaySvc != nil {
		routingModel = s.gatewaySvc.groupMappedModelForGroup(ctx, &due.GroupID, model)
	}
	return routing.GroupProbeTarget{ProviderID: provider.Record.ID, ModelID: routingModel}, nil
}

func (s Probe) Test(ctx context.Context, id int64, model, prompt, userAgent string) (*routing.ProbeExecutionResult, error) {
	result, err := s.ProviderTest.RunTestBackgroundWithPromptAndUserAgent(ctx, id, model, prompt, userAgent)
	if result == nil {
		return nil, err
	}
	return &routing.ProbeExecutionResult{Status: result.Status, LatencyMs: result.LatencyMs, ErrorMessage: result.ErrorMessage, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt}, err
}

func (s Probe) selectProbeProvider(ctx context.Context, due routing.GroupAvailabilityProbeDueGroup, modelID string) (*gatewayprovider.ExecutionProvider, error) {
	groupID := due.GroupID
	if s.openAIGateway != nil {
		return s.openAIGateway.SelectProviderForModel(ctx, &groupID, "", modelID)
	}
	if s.gatewaySvc != nil {
		return s.gatewaySvc.SelectProviderForModel(ctx, &groupID, "", modelID)
	}
	return nil, fmt.Errorf("provider selector not configured")
}
