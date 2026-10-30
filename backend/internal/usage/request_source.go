package usage

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
)

// request_source.go classifies customer and internal model work and carries
// immutable attribution through each internal run. System calls do not acquire
// customer funding identities when an administrator starts them. Their operator
// and logical parent remain separate from user, team, and API-key ownership,
// while request source stays independent of streaming transport and billing
// units used by the same analytical rows.
//
// Context publication retains cancellation and deadlines. An omitted parent
// reuses the local request identity or receives a new background identity, and
// the operator value is copied before asynchronous accounting can outlive its
// caller. Individual transports assign separate attempt identifiers beneath
// that shared parent. Readers receive a value copy and keep its referenced
// operator immutable rather than rewriting another attempt's attribution.

// Request sources classify the owner of model work independently of streaming
// transport, billing mode, and the model dimension used by analytics.
const (
	SourceUser          = "user"           // authenticated customer model request
	SourceGroupProbe    = "group_probe"    // automatic group availability request
	SourceProviderTest  = "provider_test"  // administrator initiated provider test
	SourceScheduledTest = "scheduled_test" // scheduled provider connectivity test
	SourceQuotaProbe    = "quota_probe"    // inference used for quota observation
)

// SystemUsage retains nonfinancial attribution and the completeness of one
// observed system exchange. No prompt, response content, or credential belongs here.
type SystemUsage struct {
	MappedModel     string `json:"mapped_model,omitempty"`     // model entering provider mapping
	ParentRequestID string `json:"parent_request_id"`          // owning test run identity
	OperatorUserID  *int64 `json:"operator_user_id,omitempty"` // optional initiating administrator
	Outcome         string `json:"outcome"`                    // success, failed, or canceled
	UsageState      string `json:"usage_state"`                // complete, partial, or missing
	CostState       string `json:"cost_state"`                 // calculated, partial, or unknown
	PricingModel    string `json:"pricing_model,omitempty"`    // model identity used for pricing
	PricingSource   string `json:"pricing_source,omitempty"`   // selected cost pricing origin
	HTTPStatus      int    `json:"http_status,omitempty"`      // observed upstream HTTP status
}

// SystemRequest describes one internal run before its individual upstream
// attempts receive independent request identities and measured usage.
type SystemRequest struct {
	MappedModel     string // model entering provider mapping
	Source          string // internal model request source
	ParentRequestID string // owning test run identity
	OperatorUserID  *int64 // optional initiating administrator
	GroupID         int64  // selected group or zero
	RequestedModel  string // original requested model identity
	Automatic       bool   // enforce local provider spending limits
}

// systemRequestKey prevents unrelated context values from replacing internal
// execution attribution, which is never accepted from upstream response data.
type systemRequestKey struct{}

// ValidRequestSource accepts stored sources and the two administrator filter
// selectors. Empty selects all records, while system excludes customer requests.
// Unknown strings are rejected without trimming or case normalization.
func ValidRequestSource(source string) bool {
	switch source {
	case "", "all", "system", SourceUser, SourceGroupProbe, SourceProviderTest, SourceScheduledTest, SourceQuotaProbe:
		return true
	default:
		return false
	}
}

// WithSystemRequest attaches immutable attribution to this internal execution.
// Callers retain cancellation and provide a system source. An omitted parent
// uses the current local request ID, or a new identity for background work;
// nested upstream attempts share attribution without mutable counters.
//
// The operator is copied before publication because accounting can run after
// the initiating handler returns. Existing local request identity anchors manual
// calls, while background executions receive a new root without changing their
// cancellation or deadlines. Explicit parent identities retain caller-selected
// grouping across provider and protocol retries.
func WithSystemRequest(ctx context.Context, request SystemRequest) context.Context {
	if request.OperatorUserID != nil {
		// Accounting can outlive the initiating HTTP handler. Copy its operator
		// identity now so caller-owned changes cannot rewrite deferred attribution.
		operator := *request.OperatorUserID
		request.OperatorUserID = &operator
	}
	if request.ParentRequestID == "" {
		request.ParentRequestID = telemetry.RequestIDValue(ctx)
		if request.ParentRequestID == "" {
			request.ParentRequestID = telemetry.NewRequestID()
		}
	}
	return context.WithValue(ctx, systemRequestKey{}, request)
}

// SystemRequestFromContext returns trusted internal attribution when present.
// Missing values leave ordinary gateway requests outside system accounting.
// The returned value is a copy; callers keep its referenced operator identifier
// immutable while the scoped execution remains active.
func SystemRequestFromContext(ctx context.Context) (SystemRequest, bool) {
	request, ok := ctx.Value(systemRequestKey{}).(SystemRequest)
	return request, ok
}

// EffectiveRequestSource preserves the customer interpretation of historical
// in-memory rows whose source was not supplied by their producer. An empty
// field is read as user, while explicitly supplied sources remain unchanged.
func (l *UsageLog) EffectiveRequestSource() string {
	if l.RequestSource == "" {
		return SourceUser
	}
	return l.RequestSource
}
