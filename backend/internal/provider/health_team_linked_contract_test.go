package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type teamLinkedProviderRepoStub struct {
	teamProviders []provider.Record
	listErr       error
	listCalls     int
	setErrorIDs   []int64
	setErrorMsgs  map[int64]string
	failSetError  map[int64]error
}

// ListByPlatform 返回指定平台的 active 提供商。
func (r *teamLinkedProviderRepoStub) ListByPlatform(ctx context.Context, platform string) ([]provider.Record, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]provider.Record, 0, len(r.teamProviders))
	for _, acc := range r.teamProviders {
		if acc.Platform == platform && acc.Status == provider.StatusActive {
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

func newTeamLinkedProvider(id int64, teamID string) provider.Record {
	return provider.Record{
		ID:          id,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      provider.StatusActive,
		Credentials: map[string]any{"chatgpt_account_id": teamID},
	}
}

// newTeamLinkedFixture: #1 触发者(team-A) #2 同队 #3 异队 #4 apikey #5 影子 #6 同队 #7 同队但已 error
func newTeamLinkedFixture() []provider.Record {
	parentID := int64(1)
	shadow := provider.Record{
		ID:               5,
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		Status:           provider.StatusActive,
		ParentProviderID: &parentID,
	}
	apikey := newTeamLinkedProvider(4, "team-A")
	apikey.Type = capability.ProviderTypeAPIKey
	erroredSibling := newTeamLinkedProvider(7, "team-A")
	erroredSibling.Status = provider.StatusError
	return []provider.Record{
		newTeamLinkedProvider(1, "team-A"),
		newTeamLinkedProvider(2, "team-A"),
		newTeamLinkedProvider(3, "team-B"),
		apikey,
		shadow,
		newTeamLinkedProvider(6, "team-A"),
		erroredSibling,
	}
}

func TestTeamLinkedError_DirectCallSkipsTriggerProvider(t *testing.T) {
	// 直调对应 fastpath 调用点：提供商级临时不可调度规则短路时联动仍然生效
	repo := &teamLinkedProviderRepoStub{teamProviders: newTeamLinkedFixture()}
	rl, blocker := newTeamLinkedTestService(repo)
	trigger := newTeamLinkedProvider(1, "team-A")

	rl.HandleWorkspaceDeactivated(context.Background(), &trigger, true)

	require.Equal(t, []int64{2, 6}, repo.setErrorIDs)
	require.Equal(t, []string{provider.OpenAITeamLinkedErrorBlockReason, provider.OpenAITeamLinkedErrorBlockReason}, blocker.reasons)
}

func TestTeamLinkedError_MissingTeamIDDoesNothing(t *testing.T) {
	repo := &teamLinkedProviderRepoStub{teamProviders: newTeamLinkedFixture()}
	rl, blocker := newTeamLinkedTestService(repo)
	trigger := provider.Record{ID: 9, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: provider.StatusActive}

	rl.HandleWorkspaceDeactivated(context.Background(), &trigger, true)

	require.Empty(t, repo.setErrorIDs)
	require.Empty(t, blocker.reasons)
	require.Zero(t, repo.listCalls)
}

func TestTeamLinkedError_SetErrorFailureDoesNotAbortRemaining(t *testing.T) {
	repo := &teamLinkedProviderRepoStub{
		teamProviders: newTeamLinkedFixture(),
		failSetError:  map[int64]error{2: errors.New("db down")},
	}
	rl, blocker := newTeamLinkedTestService(repo)
	trigger := newTeamLinkedProvider(1, "team-A")

	rl.HandleWorkspaceDeactivated(context.Background(), &trigger, true)

	require.Equal(t, []int64{6}, repo.setErrorIDs)
	// 进程内熔断先于落库执行，两个提供商都已被熔断
	require.Len(t, blocker.reasons, 2)
}

// newTeamLinkedTestService 为团队联动组件提供列表和写入替身。
func newTeamLinkedTestService(repo *teamLinkedProviderRepoStub) (*provider.TeamLinkedHealth, *teamBlockRecorder) {
	blocker := &teamBlockRecorder{}
	return provider.NewTeamLinkedHealth(repo, provider.TeamLinkedOptions{Block: blocker.Block}), blocker
}

type teamBlockRecorder struct {
	providers []*provider.Record
	reasons   []string
}

func (r *teamBlockRecorder) Block(value *provider.Record, _ time.Time, reason string) {
	r.providers = append(r.providers, value)
	r.reasons = append(r.reasons, reason)
}
