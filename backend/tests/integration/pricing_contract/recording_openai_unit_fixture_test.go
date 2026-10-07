package pricingcontract

import (
	"testing"

	completiontestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"

	identity "github.com/TokenFlux/TokenRouter/internal/identity"

	"github.com/TokenFlux/TokenRouter/internal/billing"

	usagecore "github.com/TokenFlux/TokenRouter/internal/usage"
	"github.com/stretchr/testify/require"
)

func requireOpenAIRecordUsageBillingRepoStub(t *testing.T, svc *completiontestkit.Recording) *completiontestkit.SettlementStore {
	t.Helper()

	billingRepo, ok := svc.Dependencies.Funds.(*completiontestkit.SettlementStore)
	require.True(t, ok)
	return billingRepo
}

// newOpenAIRecordUsageServiceForTest 使用测试存储构造 completion.Recorder 的记录夹具。
func newOpenAIRecordUsageServiceForTest(logs usagecore.UsageLogRepository, _ identity.UserRepository, _ billing.UserSubscriptionRepository, rates billing.UserGroupRateRepository) *completiontestkit.Recording {
	return completiontestkit.NewRecording(logs, &completiontestkit.SettlementStore{}, rates, true)
}
