package ops

import "time"

// PlatformConcurrencyInfo aggregates concurrency usage by platform.
type PlatformConcurrencyInfo struct {
	Platform       string  `json:"platform"`
	CurrentInUse   int64   `json:"current_in_use"`
	MaxCapacity    int64   `json:"max_capacity"`
	LoadPercentage float64 `json:"load_percentage"`
	WaitingInQueue int64   `json:"waiting_in_queue"`
}

// GroupConcurrencyInfo aggregates concurrency usage by group.
//
// Note: one provider can belong to multiple groups; group totals are therefore not additive across groups.
type GroupConcurrencyInfo struct {
	GroupID        int64   `json:"group_id"`
	GroupName      string  `json:"group_name"`
	CurrentInUse   int64   `json:"current_in_use"`
	MaxCapacity    int64   `json:"max_capacity"`
	LoadPercentage float64 `json:"load_percentage"`
	WaitingInQueue int64   `json:"waiting_in_queue"`
}

// ProviderConcurrencyInfo represents real-time concurrency usage for a single provider.
type ProviderConcurrencyInfo struct {
	ProviderID     int64   `json:"provider_id"`
	ProviderName   string  `json:"provider_name"`
	Platform       string  `json:"platform"`
	GroupID        int64   `json:"group_id"`
	GroupName      string  `json:"group_name"`
	CurrentInUse   int64   `json:"current_in_use"`
	MaxCapacity    int64   `json:"max_capacity"`
	LoadPercentage float64 `json:"load_percentage"`
	WaitingInQueue int64   `json:"waiting_in_queue"`
}

// UserConcurrencyInfo represents real-time concurrency usage for a single user.
type UserConcurrencyInfo struct {
	UserID         int64   `json:"user_id"`
	UserEmail      string  `json:"user_email"`
	Username       string  `json:"username"`
	CurrentInUse   int64   `json:"current_in_use"`
	MaxCapacity    int64   `json:"max_capacity"`
	LoadPercentage float64 `json:"load_percentage"`
	WaitingInQueue int64   `json:"waiting_in_queue"`
}

// PlatformAvailability aggregates provider availability by platform.
type PlatformAvailability struct {
	Platform       string `json:"platform"`
	TotalProviders int64  `json:"total_providers"`
	AvailableCount int64  `json:"available_count"`
	RateLimitCount int64  `json:"rate_limit_count"`
	ErrorCount     int64  `json:"error_count"`
}

// GroupAvailability aggregates provider availability by group.
type GroupAvailability struct {
	GroupID        int64  `json:"group_id"`
	GroupName      string `json:"group_name"`
	TotalProviders int64  `json:"total_providers"`
	AvailableCount int64  `json:"available_count"`
	RateLimitCount int64  `json:"rate_limit_count"`
	ErrorCount     int64  `json:"error_count"`
}

// ProviderAvailability represents current availability for a single provider.
type ProviderAvailability struct {
	ProviderID   int64  `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	Platform     string `json:"platform"`
	GroupID      int64  `json:"group_id"`
	GroupName    string `json:"group_name"`

	Status string `json:"status"`

	IsAvailable   bool `json:"is_available"`
	IsRateLimited bool `json:"is_rate_limited"`
	IsOverloaded  bool `json:"is_overloaded"`
	HasError      bool `json:"has_error"`

	RateLimitResetAt       *time.Time `json:"rate_limit_reset_at"`
	RateLimitRemainingSec  *int64     `json:"rate_limit_remaining_sec"`
	OverloadUntil          *time.Time `json:"overload_until"`
	OverloadRemainingSec   *int64     `json:"overload_remaining_sec"`
	ErrorMessage           string     `json:"error_message"`
	TempUnschedulableUntil *time.Time `json:"temp_unschedulable_until,omitempty"`
}
