package opencode

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream/internal/usageclient"
	"github.com/TokenFlux/TokenRouter/internal/upstream/usagecontract"
	"github.com/TokenFlux/TokenRouter/internal/upstream/usageview"
)

// usage.go reads the three OpenCode Go allowance windows with API-key
// authentication. The upstream supplies percentages, status, and reset instants;
// normalization preserves overuse and keeps these values separate from wallet
// money. Invalid or missing windows reject the observation instead of appearing
// as an unused subscription. Provider orchestration owns query deduplication,
// identity rechecks, and display caching around this stateless adapter.

// GoUsageAdapter implements the fixed Go allowance endpoint and normalized
// percentage presentation used by the administrator's upstream usage controls.
type GoUsageAdapter struct{}

// goUsageMeter decodes one complete allowance observation from OpenCode.
type goUsageMeter struct {
	Percent  *float64 `json:"percent"`  // observed used percentage
	Status   string   `json:"status"`   // upstream allowance status
	ResetsAt string   `json:"resetsAt"` // RFC3339 reset instant
}

// Name returns the registered identifier for OpenCode Go allowance queries.
func (*GoUsageAdapter) Name() string { return usageview.UpstreamUsageAdapterOpenCodeGo }

// Query reads Go allowances from the validated management root and API key.
// Every expected window must be present and well formed. Authentication,
// throttling, transport, and bounded-body errors propagate through shared usage
// errors, leaving query orchestration responsible for the provider identity.
func (*GoUsageAdapter) Query(ctx context.Context, input *usagecontract.Request) (*usageview.UpstreamUsageInfo, error) {
	client := usageclient.New(input)
	body, status, err := client.GetURL(ctx, strings.TrimRight(input.BaseURL, "/")+"/zen/go/v1/usage", true)
	if err != nil {
		return nil, err
	}
	if err = usageclient.UpstreamUsageHTTPError(status, true); err != nil {
		return nil, err
	}
	return ParseGoUsage(body)
}

// ParseGoUsage returns three percentage windows or an invalid-response error.
// Percentages above one hundred retain their original used value while derived
// remaining allowance stops at zero. Reset instants are normalized to UTC.
func ParseGoUsage(body []byte) (*usageview.UpstreamUsageInfo, error) {
	var response struct {
		Usage map[string]goUsageMeter `json:"usage"` // reported allowance windows
	}
	if json.Unmarshal(body, &response) != nil {
		return nil, usageview.ErrUpstreamUsageInvalidResponse
	}
	result := &usageview.UpstreamUsageInfo{Provider: "OpenCode Go", Mode: "limits", Unit: "PERCENT"}
	for _, window := range []struct {
		key  string // upstream window identity
		name string // normalized window label
	}{{"rolling", "5h"}, {"weekly", "7d"}, {"monthly", "monthly"}} {
		meter, found := response.Usage[window.key]
		if !found || meter.Percent == nil || math.IsNaN(*meter.Percent) || math.IsInf(*meter.Percent, 0) || *meter.Percent < 0 || (meter.Status != "ok" && meter.Status != "rate-limited") {
			return nil, usageview.ErrUpstreamUsageInvalidResponse
		}
		reset, err := time.Parse(time.RFC3339Nano, meter.ResetsAt)
		if err != nil || reset.IsZero() {
			return nil, usageview.ErrUpstreamUsageInvalidResponse
		}
		reset = reset.UTC()
		// Percentage windows express their total allowance as one hundred.
		limit := 100.0
		// The upstream can report overshoot after a completed request. Preserve used
		// percentage and floor only the derived remainder displayed to the administrator.
		remaining := max(0, limit-*meter.Percent)
		result.Limits = append(result.Limits, usageview.UpstreamUsageLimit{Name: window.name, Used: meter.Percent, Limit: &limit, Remaining: &remaining, ResetAt: &reset, Status: meter.Status})
	}
	return result, nil
}
