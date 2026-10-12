//go:build integration

package provider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// GatewayRoutingSuite 测试网关路由相关的数据库查询
// 验证提供商选择和分流逻辑在真实数据库环境下的行为。
type GatewayRoutingSuite struct {
	suite.Suite
	ctx          context.Context
	client       *dbent.Client
	providerRepo *providerpostgres.ProviderStore
}

func (s *GatewayRoutingSuite) SetupTest() {
	s.ctx = context.Background()
	tx := testEntTx(s.T())
	s.client = tx.Client()
	s.providerRepo = newProviderStoreContract(s.client, tx, nil)
}

func TestGatewayRoutingSuite(t *testing.T) {
	suite.Run(t, new(GatewayRoutingSuite))
}

// TestListSchedulableByPlatforms_GeminiAndAntigravity 验证多平台提供商查询。
func (s *GatewayRoutingSuite) TestListSchedulableByPlatforms_GeminiAndAntigravity() {
	// 创建各平台提供商
	geminiAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "gemini-oauth",
		Platform:    capability.PlatformGemini,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Priority:    1,
	})

	antigravityAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "antigravity-oauth",
		Platform:    capability.PlatformAntigravity,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Priority:    2,
		Credentials: map[string]any{
			"access_token":  "test-token",
			"refresh_token": "test-refresh",
			"project_id":    "test-project",
		},
	})

	// 创建不应被选中的 anthropic 提供商
	mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "anthropic-oauth",
		Platform:    capability.PlatformAnthropic,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Priority:    0,
	})

	// 查询 gemini + antigravity 平台
	providers, err := s.providerRepo.ListSchedulableByPlatforms(s.ctx, []string{
		capability.PlatformGemini,
		capability.PlatformAntigravity,
	})

	s.Require().NoError(err)
	s.Require().Len(providers, 2, "应返回 gemini 和 antigravity 两个提供商")

	// 验证返回的提供商平台
	platforms := make(map[string]bool)
	for _, acc := range providers {
		platforms[acc.Platform] = true
	}
	s.Require().True(platforms[capability.PlatformGemini], "应包含 gemini 提供商")
	s.Require().True(platforms[capability.PlatformAntigravity], "应包含 antigravity 提供商")
	s.Require().False(platforms[capability.PlatformAnthropic], "不应包含 anthropic 提供商")

	// 验证提供商 ID 匹配
	ids := make(map[int64]bool)
	for _, acc := range providers {
		ids[acc.ID] = true
	}
	s.Require().True(ids[geminiAcc.ID])
	s.Require().True(ids[antigravityAcc.ID])
}

// TestListSchedulableByGroupIDAndPlatforms_WithGroupBinding 验证按分组过滤。
func (s *GatewayRoutingSuite) TestListSchedulableByGroupIDAndPlatforms_WithGroupBinding() {
	// 创建可关联不同平台提供商的分组
	group := mustCreateGroup(s.T(), s.client, &routing.Group{
		Name:   "mixed-group",
		Status: billing.StatusActive,
	})

	// 创建提供商
	boundAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "bound-antigravity",
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
	})
	unboundAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "unbound-antigravity",
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
	})

	// 只绑定一个提供商到分组
	mustBindProviderToGroup(s.T(), s.client, boundAcc.ID, group.ID)

	// 查询分组内的提供商
	providers, err := s.providerRepo.ListSchedulableByGroupIDAndPlatforms(s.ctx, group.ID, []string{
		capability.PlatformGemini,
		capability.PlatformAntigravity,
	})

	s.Require().NoError(err)
	s.Require().Len(providers, 1, "应只返回绑定到分组的提供商")
	s.Require().Equal(boundAcc.ID, providers[0].ID)

	// 确认未绑定的提供商不在结果中
	for _, acc := range providers {
		s.Require().NotEqual(unboundAcc.ID, acc.ID, "不应包含未绑定的提供商")
	}
}

// TestListSchedulableByPlatform_Antigravity 验证单平台查询。
func (s *GatewayRoutingSuite) TestListSchedulableByPlatform_Antigravity() {
	// 创建多种平台提供商
	mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "gemini-1",
		Platform:    capability.PlatformGemini,
		Status:      billing.StatusActive,
		Schedulable: true,
	})

	antigravity := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "antigravity-1",
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
	})

	// 只查询 antigravity 平台
	providers, err := s.providerRepo.ListSchedulableByPlatform(s.ctx, capability.PlatformAntigravity)

	s.Require().NoError(err)
	s.Require().Len(providers, 1)
	s.Require().Equal(antigravity.ID, providers[0].ID)
	s.Require().Equal(capability.PlatformAntigravity, providers[0].Platform)
}

// TestSchedulableFilter_ExcludesInactive 验证不可调度提供商被过滤。
func (s *GatewayRoutingSuite) TestSchedulableFilter_ExcludesInactive() {
	// 创建可调度提供商
	activeAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "active-antigravity",
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
	})

	// 创建不可调度提供商（需要先创建再更新，因为 fixture 默认设置 Schedulable=true）
	inactiveAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:     "inactive-antigravity",
		Platform: capability.PlatformAntigravity,
		Status:   billing.StatusActive,
	})
	s.Require().NoError(s.client.Provider.UpdateOneID(inactiveAcc.ID).SetSchedulable(false).Exec(s.ctx))

	// 创建错误状态提供商
	mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "error-antigravity",
		Platform:    capability.PlatformAntigravity,
		Status:      providercore.StatusError,
		Schedulable: true,
	})

	providers, err := s.providerRepo.ListSchedulableByPlatform(s.ctx, capability.PlatformAntigravity)

	s.Require().NoError(err)
	s.Require().Len(providers, 1, "应只返回可调度的 active 提供商")
	s.Require().Equal(activeAcc.ID, providers[0].ID)
}

// TestPlatformRoutingDecision 验证平台路由决策
// 这个测试模拟 Handler 层在选择提供商后的路由决策逻辑。
func (s *GatewayRoutingSuite) TestPlatformRoutingDecision() {
	// 创建两种平台的提供商
	geminiAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "gemini-route-test",
		Platform:    capability.PlatformGemini,
		Status:      billing.StatusActive,
		Schedulable: true,
	})

	antigravityAcc := mustCreateProvider(s.T(), s.client, &providercore.Record{
		Name:        "antigravity-route-test",
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
	})

	tests := []struct {
		name            string
		providerID      int64
		expectedService string
	}{
		{
			name:            "Gemini提供商路由到ForwardNative",
			providerID:      geminiAcc.ID,
			expectedService: "GeminiMessagesCompatService.ForwardNative",
		},
		{
			name:            "Antigravity提供商路由到ForwardGemini",
			providerID:      antigravityAcc.ID,
			expectedService: "AntigravityGatewayService.ForwardGemini",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// 从数据库获取提供商
			provider, err := s.providerRepo.GetByID(s.ctx, tt.providerID)
			s.Require().NoError(err)

			// 模拟 Handler 层的路由决策
			var routedService string
			if provider.Platform == capability.PlatformAntigravity {
				routedService = "AntigravityGatewayService.ForwardGemini"
			} else {
				routedService = "GeminiMessagesCompatService.ForwardNative"
			}

			s.Require().Equal(tt.expectedService, routedService)
		})
	}
}
