package codec

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

type providerWire struct {
	ID                      int64
	Name                    string
	Notes                   *string
	Platform                string
	Type                    string
	Credentials             map[string]any
	Extra                   map[string]any
	ProxyID                 *int64
	ProxyFallbackOriginID   *int64
	ProxyFallbackOriginName *string
	Concurrency             int
	Priority                int
	RateMultiplier          *float64
	LoadFactor              *int
	Status                  string
	ErrorMessage            string
	LastUsedAt              *time.Time
	ExpiresAt               *time.Time
	AutoPauseOnExpired      bool
	CreatedAt               time.Time
	UpdatedAt               time.Time
	Schedulable             bool
	RateLimitedAt           *time.Time
	RateLimitResetAt        *time.Time
	OverloadUntil           *time.Time
	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason string
	QuotaAutoPaused         bool `json:"-"`
	SessionWindowStart      *time.Time
	SessionWindowEnd        *time.Time
	SessionWindowStatus     string
	ParentProviderID        *int64
	QuotaDimension          string
	Proxy                   *egress.Proxy
	ProviderGroups          []providerGroupWire
	GroupIDs                []int64
	Groups                  []*groupWire
}

// groupWire 编码分组配置，ProviderGroups 写为 null，以兼容缓存格式。
type groupWire struct {
	*accessview.GroupConfig
	ProviderGroups []json.RawMessage
}
type providerGroupWire struct {
	ProviderID int64
	GroupID    int64
	CreatedAt  time.Time
	Provider   *providerWire
	Group      *groupWire
}

// recordSnapshot 包装提供商记录，调度器通过 SnapshotMetadata 读取重建元数据。
// 执行适配器通过 RecordValue 取得含凭据的完整记录。
type recordSnapshot struct{ value *provider.Record }

func encodeGroup(group *accessview.GroupConfig) *groupWire {
	if group == nil {
		return nil
	}
	return &groupWire{GroupConfig: group}
}

func decodeGroup(group *groupWire) *accessview.GroupConfig {
	if group == nil {
		return nil
	}
	return group.GroupConfig
}

// MarshalProviderRecord 保留完整快照中的凭据、额外字段及 nil/空关系集合。
func MarshalProviderRecord(value *provider.Record) ([]byte, error) {
	return json.Marshal(recordToWire(value, make(map[*provider.Record]*providerWire)))
}

func recordToWire(value *provider.Record, seen map[*provider.Record]*providerWire) *providerWire {
	if value == nil {
		return nil
	}
	if previous, ok := seen[value]; ok {
		return previous
	}
	out := &providerWire{}
	seen[value] = out
	out.ID = value.ID
	out.Name = value.Name
	out.Notes = value.Notes
	out.Platform = value.Platform
	out.Type = value.Type
	out.Credentials = value.Credentials
	out.Extra = value.Extra
	out.ProxyID = value.ProxyID
	out.ProxyFallbackOriginID = value.ProxyFallbackOriginID
	out.ProxyFallbackOriginName = value.ProxyFallbackOriginName
	out.Concurrency = value.Concurrency
	out.Priority = value.Priority
	out.RateMultiplier = value.RateMultiplier
	out.LoadFactor = value.LoadFactor
	out.Status = value.Status
	out.ErrorMessage = value.ErrorMessage
	out.LastUsedAt = value.LastUsedAt
	out.ExpiresAt = value.ExpiresAt
	out.AutoPauseOnExpired = value.AutoPauseOnExpired
	out.CreatedAt = value.CreatedAt
	out.UpdatedAt = value.UpdatedAt
	out.Schedulable = value.Schedulable
	out.RateLimitedAt = value.RateLimitedAt
	out.RateLimitResetAt = value.RateLimitResetAt
	out.OverloadUntil = value.OverloadUntil
	out.TempUnschedulableUntil = value.TempUnschedulableUntil
	out.TempUnschedulableReason = value.TempUnschedulableReason
	out.QuotaAutoPaused = value.QuotaAutoPaused
	out.SessionWindowStart = value.SessionWindowStart
	out.SessionWindowEnd = value.SessionWindowEnd
	out.SessionWindowStatus = value.SessionWindowStatus
	out.ParentProviderID = value.ParentProviderID
	out.QuotaDimension = value.QuotaDimension
	out.Proxy = value.Proxy
	out.GroupIDs = value.GroupIDs

	if value.Groups != nil {
		out.Groups = make([]*groupWire, len(value.Groups))
		for i, g := range value.Groups {
			out.Groups[i] = encodeGroup(g)
		}
	}
	if value.ProviderGroups != nil {
		out.ProviderGroups = make([]providerGroupWire, len(value.ProviderGroups))
		for i, g := range value.ProviderGroups {
			out.ProviderGroups[i] = providerGroupWire{ProviderID: g.ProviderID, GroupID: g.GroupID, CreatedAt: g.CreatedAt, Provider: recordToWire(g.Provider, seen), Group: encodeGroup(g.Group)}
		}
	}
	return out
}

// UnmarshalProviderRecord 从缓存报文恢复完整提供商记录，供执行适配器读取。
func UnmarshalProviderRecord(payload []byte) (*provider.Record, error) {
	var wire providerWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, err
	}
	return recordFromWire(&wire), nil
}

func recordFromWire(value *providerWire) *provider.Record {
	if value == nil {
		return nil
	}
	out := &provider.Record{Now: time.Now, LoadLocation: time.LoadLocation}
	out.ID = value.ID
	out.Name = value.Name
	out.Notes = value.Notes
	out.Platform = value.Platform
	out.Type = value.Type
	out.Credentials = value.Credentials
	out.Extra = value.Extra
	out.ProxyID = value.ProxyID
	out.ProxyFallbackOriginID = value.ProxyFallbackOriginID
	out.ProxyFallbackOriginName = value.ProxyFallbackOriginName
	out.Concurrency = value.Concurrency
	out.Priority = value.Priority
	out.RateMultiplier = value.RateMultiplier
	out.LoadFactor = value.LoadFactor
	out.Status = value.Status
	out.ErrorMessage = value.ErrorMessage
	out.LastUsedAt = value.LastUsedAt
	out.ExpiresAt = value.ExpiresAt
	out.AutoPauseOnExpired = value.AutoPauseOnExpired
	out.CreatedAt = value.CreatedAt
	out.UpdatedAt = value.UpdatedAt
	out.Schedulable = value.Schedulable
	out.RateLimitedAt = value.RateLimitedAt
	out.RateLimitResetAt = value.RateLimitResetAt
	out.OverloadUntil = value.OverloadUntil
	out.TempUnschedulableUntil = value.TempUnschedulableUntil
	out.TempUnschedulableReason = value.TempUnschedulableReason
	out.QuotaAutoPaused = value.QuotaAutoPaused
	out.SessionWindowStart = value.SessionWindowStart
	out.SessionWindowEnd = value.SessionWindowEnd
	out.SessionWindowStatus = value.SessionWindowStatus
	out.ParentProviderID = value.ParentProviderID
	out.QuotaDimension = value.QuotaDimension
	out.Proxy = value.Proxy
	out.GroupIDs = value.GroupIDs

	if value.Groups != nil {
		out.Groups = make([]*accessview.GroupConfig, len(value.Groups))
		for i, g := range value.Groups {
			out.Groups[i] = decodeGroup(g)
		}
	}
	if value.ProviderGroups != nil {
		out.ProviderGroups = make([]provider.GroupMembership, len(value.ProviderGroups))
		for i, g := range value.ProviderGroups {
			out.ProviderGroups[i] = provider.GroupMembership{ProviderID: g.ProviderID, GroupID: g.GroupID, CreatedAt: g.CreatedAt, Provider: recordFromWire(g.Provider), Group: decodeGroup(g.Group)}
		}
	}
	return out
}

func (s recordSnapshot) SnapshotMetadata() scheduler.SnapshotMetadata {
	return scheduler.SnapshotMetadata{
		ID: s.value.ID, Name: s.value.Name, Platform: s.value.Platform,
		GroupIDs: slices.Clone(s.value.GroupIDs),
	}
}

func WrapRecord(value *provider.Record) scheduler.SnapshotProvider {
	if value == nil {
		return nil
	}
	return recordSnapshot{value: value}
}

// RecordValue 为提供商读取和缓存编码返回完整记录，记录含有执行凭据。
func RecordValue(value scheduler.SnapshotProvider) (*provider.Record, error) {
	if value == nil {
		return nil, nil
	}
	stored, ok := value.(recordSnapshot)
	if !ok {
		return nil, fmt.Errorf("unexpected legacy scheduler snapshot data %T", value)
	}
	return stored.value, nil
}
