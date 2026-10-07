package provider_test

import (
	"context"
	"net/http"
	"testing"
	time "time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

const teamLinkedDeactivatedBody = `{"detail":{"code":"deactivated_workspace","message":"This workspace has been deactivated."}}`

type teamLinkedProviderRepoStub struct {
	gatewaytestkit.HealthStoreBase
	teamProviders []gatewayprovider.ExecutionProvider
	listErr       error
	listCalls     int
	setErrorIDs   []int64
	setErrorMsgs  map[int64]string
	failSetError  map[int64]error
}

// ListByPlatform 返回指定平台的 active 提供商。
func (r *teamLinkedProviderRepoStub) ListByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]gatewayprovider.ExecutionProvider, 0, len(r.teamProviders))
	for _, acc := range r.teamProviders {
		if acc.Record.Platform == platform && acc.Record.Status == billing.StatusActive {
			out = append(out, acc)
		}
	}
	return out, nil
}

func (r *teamLinkedProviderRepoStub) SetError(ctx context.Context, id int64, errorMsg string) error {
	if err, ok := r.failSetError[id]; ok {
		return err
	}
	r.setErrorIDs = append(r.setErrorIDs, id)
	if r.setErrorMsgs == nil {
		r.setErrorMsgs = make(map[int64]string)
	}
	r.setErrorMsgs[id] = errorMsg
	return nil
}

func newTeamLinkedProvider(id int64, teamID string) gatewayprovider.ExecutionProvider {
	return gatewayprovider.ExecutionProvider{
		Record: provider.Record{
			LoadLocation: time.LoadLocation, ID: id,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Credentials: map[string]any{"chatgpt_account_id": teamID},
		},
	}
}

// newTeamLinkedFixture: #1 触发者(team-A) #2 同队 #3 异队 #4 apikey #5 影子 #6 同队 #7 同队但已 error
func newTeamLinkedFixture() []gatewayprovider.ExecutionProvider {
	parentID := int64(1)
	shadow := gatewayprovider.ExecutionProvider{
		Record: provider.Record{
			LoadLocation: time.LoadLocation, ID: 5,
			Platform:         capability.PlatformOpenAI,
			Type:             capability.ProviderTypeOAuth,
			Status:           billing.StatusActive,
			ParentProviderID: &parentID,
		},
	}
	apikey := newTeamLinkedProvider(4, "team-A")
	apikey.Record.Type = capability.ProviderTypeAPIKey
	erroredSibling := newTeamLinkedProvider(7, "team-A")
	erroredSibling.Record.Status = provider.StatusError
	return []gatewayprovider.ExecutionProvider{
		newTeamLinkedProvider(1, "team-A"),
		newTeamLinkedProvider(2, "team-A"),
		newTeamLinkedProvider(3, "team-B"),
		apikey,
		shadow,
		newTeamLinkedProvider(6, "team-A"),
		erroredSibling,
	}
}

func newTeamLinkedTestService(repo *teamLinkedProviderRepoStub) (*provideradapter.UpstreamHealth,
	*gatewaytestkit.RuntimeBlockRecorder,
) {
	blocker := &gatewaytestkit.RuntimeBlockRecorder{}
	rl := newUpstreamHealthForTest(repo, &config.Config{}, nil, provider.HealthOptions{Block: func(v *provider.Record, until time.Time, reason string) {
		blocker.BlockProviderScheduling(gatewayprovider.NewExecutionProvider(v), until, reason)
	}}, nil)

	return rl, blocker
}

func TestTeamLinkedError_FanoutMarksSameTeamProviders(t *testing.T) {
	repo := &teamLinkedProviderRepoStub{teamProviders: newTeamLinkedFixture()}
	rl, blocker := newTeamLinkedTestService(repo)
	trigger := newTeamLinkedProvider(1, "team-A")

	shouldDisable := gatewayprovider.ApplyExecutionHealth(context.Background(), rl, &trigger, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusPaymentRequired, http.Header{}, []byte(teamLinkedDeactivatedBody), nil)).StopScheduling

	require.True(t, shouldDisable)
	// fan-out 先标记同队兄弟（#2、#6），触发提供商 #1 随后由常规 case 402 标记
	require.Equal(t, []int64{2, 6, 1}, repo.setErrorIDs)
	require.Contains(t, repo.setErrorMsgs[2], "team-linked error triggered by provider #1")
	require.Contains(t, repo.setErrorMsgs[6], "team-linked error triggered by provider #1")
	require.Contains(t, repo.setErrorMsgs[1], "Workspace deactivated (402)")
	require.NotContains(t, repo.setErrorMsgs[1], "team-linked")
	// 熔断顺序：兄弟提供商先于落库全部进程内熔断，触发提供商走 auth_error
	require.Equal(t, []string{provider.OpenAITeamLinkedErrorBlockReason, provider.OpenAITeamLinkedErrorBlockReason, "auth_error"}, blocker.Reasons)
	require.Equal(t, int64(2), blocker.Providers[0].Record.ID)
	require.Equal(t, int64(6), blocker.Providers[1].Record.ID)
	require.Equal(t, int64(1), blocker.Providers[2].Record.ID)
}

func TestTeamLinkedError_GenericPaymentErrorDoesNotFanout(t *testing.T) {
	repo := &teamLinkedProviderRepoStub{teamProviders: newTeamLinkedFixture()}
	rl, _ := newTeamLinkedTestService(repo)
	trigger := newTeamLinkedProvider(1, "team-A")

	gatewayprovider.ApplyExecutionHealth(context.Background(), rl, &trigger, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusPaymentRequired, http.Header{}, []byte(`{"error":{"message":"insufficient balance"}}`), nil))

	require.Equal(t, []int64{1}, repo.setErrorIDs)
	require.Contains(t, repo.setErrorMsgs[1], "Payment required (402)")
	require.Zero(t, repo.listCalls)
}

func TestTeamLinkedError_DedupWithinTTL(t *testing.T) {
	repo := &teamLinkedProviderRepoStub{teamProviders: newTeamLinkedFixture()}
	rl, _ := newTeamLinkedTestService(repo)
	first := newTeamLinkedProvider(1, "team-A")
	second := newTeamLinkedProvider(2, "team-A")

	gatewayprovider.ApplyExecutionHealth(context.Background(), rl, &first, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusPaymentRequired, http.Header{}, []byte(teamLinkedDeactivatedBody), nil))
	gatewayprovider.ApplyExecutionHealth(context.Background(), rl, &second, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusPaymentRequired, http.Header{}, []byte(teamLinkedDeactivatedBody), nil))

	// 第二次触发被去重：只有 #2 自身经 case 402 标记，未再次 fan-out
	require.Equal(t, []int64{2, 6, 1, 2}, repo.setErrorIDs)
	require.Equal(t, 1, repo.listCalls)
}

func TestTeamLinkedError_APIKeyTriggerDoesNotFanout(t *testing.T) {
	repo := &teamLinkedProviderRepoStub{teamProviders: newTeamLinkedFixture()}
	rl, _ := newTeamLinkedTestService(repo)
	trigger := newTeamLinkedProvider(4, "team-A")
	trigger.Record.Type = capability.ProviderTypeAPIKey

	gatewayprovider.ApplyExecutionHealth(context.Background(), rl, &trigger, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusPaymentRequired, http.Header{}, []byte(teamLinkedDeactivatedBody), nil))

	require.Equal(t, []int64{4}, repo.setErrorIDs)
	require.Zero(t, repo.listCalls)
}
