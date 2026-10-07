package selection

import (
	"context"
	"testing"
	"time"

	logging "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	scheduler "github.com/TokenFlux/TokenRouter/internal/scheduler"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	routing "github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// newBedrockRoutingTestProvider 用虚构凭据构造测试用可调度提供商。
func newBedrockRoutingTestProvider(id int64, region string, forceGlobal bool) gatewayprovider.ExecutionProvider {
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: id, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeBedrock,
			Status: billing.StatusActive, Schedulable: true, Concurrency: 5, Priority: int(id),
			Credentials: map[string]any{
				"aws_region": region, "auth_mode": "sigv4",
				"aws_access_key_id": "test-akid", "aws_secret_access_key": "test-secret",
			},
		},
	}
	if forceGlobal {
		provider.Record.Credentials["aws_force_global"] = "true"
	}
	return provider
}

// 正式转发与管理员测试必须使用相同 ID；全局推理不改变来源端点和 SigV4 签名范围。

// 无有效路由时不调用上游或写提供商状态，管理员仅在确有全局能力时收到开启提示。

// TestBedrockRegionRouting_SchedulerAndDiagnosisAgree 验证地域不支持的粘性提供商必须被跳过，提供商筛选和错误诊断应使用相同的区域规则。
func TestBedrockRegionRouting_SchedulerAndDiagnosisAgree(t *testing.T) {
	groupID := int64(5200)
	invalid := newBedrockRoutingTestProvider(1, "ap-northeast-1", false)
	valid := newBedrockRoutingTestProvider(2, "us-east-1", false)
	for _, loadBatchEnabled := range []bool{false, true} {
		for _, withValid := range []bool{false, true} {
			name := "全部无效"
			if withValid {
				name = "存在有效提供商"
			}
			if loadBatchEnabled {
				name += "批量负载"
			}
			t.Run(name, func(t *testing.T) {
				providers := []gatewayprovider.ExecutionProvider{invalid}
				if withValid {
					providers = append(providers, valid)
				}
				repo := &mockProviderRepoForPlatform{providers: providers, providersByID: map[int64]*gatewayprovider.ExecutionProvider{}}
				for i := range repo.providers {
					repo.providers[i].Record.ProviderGroups = []providercore.GroupMembership{{ProviderID: repo.providers[i].Record.ID, GroupID: groupID}}
					repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
				}
				group := &routing.Group{ID: groupID, Status: billing.StatusActive, Hydrated: true}
				cfg := testConfig()
				cfg.Gateway.Scheduling.LoadBatchEnabled = loadBatchEnabled
				gateway := newGenericSelectionForTest(GenericDependencies{
					Reads: Reads{
						Providers: repo,

						Groups: &mockGroupRepoForGateway{groups: map[int64]*routing.Group{groupID: group}},
					},
					Shared: Shared{
						Cache:       &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"sticky": 1}},
						Concurrency: scheduler.NewConcurrencyService(&mockConcurrencyCache{}, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
					},
				}, cfg)

				diagnosis := gatewayprovider.NewModelAvailability(selectionAvailabilityFixture{repo}, nil, false).DiagnoseGeneral(context.Background(), &groupID, "claude-sonnet-5", capability.PlatformAnthropic)
				require.True(t, diagnosis.HasProvidersInPool)
				require.Equal(t, withValid, diagnosis.HasModelSupport)
				selected, err := gateway.SelectProviderWithLoadAwareness(context.Background(), &groupID, "sticky", "claude-sonnet-5", nil, "", 0)
				if withValid {
					require.NoError(t, err)
					require.Equal(t, valid.Record.ID, selected.Provider.Record.ID)
					if selected.ReleaseFunc != nil {
						selected.ReleaseFunc()
					}
				} else {
					require.Error(t, err)
					require.Nil(t, selected)
				}
			})
		}
	}
}

// 模型广场通过公共模型解析器获得相同的区域可用性，不自行回退到默认目录。

// bedrockMarketplaceGroups 仅提供区域路由回归所需的分组数据。

func (m *mockProviderRepoForPlatform) availabilityRecords(_ context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]gatewayprovider.ExecutionProvider, error) {
	platformSet := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		platformSet[platform] = struct{}{}
	}
	result := make([]gatewayprovider.ExecutionProvider, 0, len(m.providers))
	for _, acc := range m.providers {
		if _, ok := platformSet[acc.Record.Platform]; !ok || acc.Record.Status != billing.StatusActive || !acc.Record.Schedulable {
			continue
		}
		if groupID != nil {
			inGroup := false
			for _, providerGroup := range acc.Record.ProviderGroups {
				if providerGroup.GroupID == *groupID {
					inGroup = true
					break
				}
			}
			if !inGroup {
				continue
			}
		} else if !includeGrouped && (len(acc.Record.ProviderGroups) > 0 || len(acc.Record.GroupIDs) > 0) {
			continue
		}
		result = append(result, acc)
	}
	return result, nil
}

// selectionAvailabilityFixture 筛选候选并返回模型诊断需要的记录。
type selectionAvailabilityFixture struct{ *mockProviderRepoForPlatform }

func (s selectionAvailabilityFixture) ListModelAvailabilityCandidates(ctx context.Context, group *int64, platforms []string, all bool) ([]providercore.Record, error) {
	values, err := s.availabilityRecords(ctx, group, platforms, all)
	out := make([]providercore.Record, len(values))
	for i := range values {
		out[i] = values[i].Record
	}
	return out, err
}
