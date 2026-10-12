package provider

type CreateProviderInput struct {
	Name               string
	Notes              *string
	Platform           string
	Type               string
	Credentials        map[string]any
	Extra              map[string]any
	ProxyID            *int64
	Concurrency        int
	Priority           int
	RateMultiplier     *float64 // 提供商计费倍率（>=0，允许 0）
	LoadFactor         *int
	GroupIDs           []int64
	ExpiresAt          *int64
	AutoPauseOnExpired *bool
}

// ShadowOptions 描述影子创建参数；影子不持认证凭据。
type ShadowOptions struct {
	Name        string
	Priority    int
	Concurrency int
	GroupIDs    []int64
}

type UpdateProviderInput struct {
	// PatchCredentials 只由字段维护用例设置，HTTP 不能借此写任意完整凭据。
	PatchCredentials bool `json:"-"`
	// PatchExtra 仅用于维护字段，保留锁内最新的无关管理配置。
	PatchExtra bool `json:"-"`
	// ExpectedCredentials 只由内部刷新入口提供，HTTP 输入不能设置。
	ExpectedCredentials *CredentialVersion `json:"-"`
	Name                string
	Notes               *string
	Type                string // Provider type: oauth, setup-token, apikey
	Credentials         map[string]any
	Extra               map[string]any
	ProxyID             *int64
	Concurrency         *int     // 使用指针区分"未提供"和"设置为0"
	Priority            *int     // 使用指针区分"未提供"和"设置为0"
	RateMultiplier      *float64 // 提供商计费倍率（>=0，允许 0）
	LoadFactor          *int
	Status              string
	GroupIDs            *[]int64
	ExpiresAt           *int64
	AutoPauseOnExpired  *bool
}

// BulkUpdateProvidersInput 指定批量修改的筛选条件和待更新字段，省略的字段保持不变。
type BulkUpdateProvidersInput struct {
	ProviderIDs    []int64
	Filters        *BulkUpdateProviderFilters
	Name           string
	ProxyID        *int64
	Concurrency    *int
	Priority       *int
	RateMultiplier *float64 // 提供商计费倍率（>=0，允许 0）
	LoadFactor     *int
	Status         string
	Schedulable    *bool
	GroupIDs       *[]int64
	Credentials    map[string]any
	Extra          map[string]any
}

type BulkUpdateProviderFilters struct {
	Platform    string
	Type        string
	Status      string
	Group       string
	Search      string
	PrivacyMode string
}

// BulkUpdateProviderResult 保留单提供商修改结果。
type BulkUpdateProviderResult struct {
	ProviderID int64  `json:"provider_id"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// BulkUpdateProvidersResult 保留批量结果的顺序与计数。
type BulkUpdateProvidersResult struct {
	Success    int                        `json:"success"`
	Failed     int                        `json:"failed"`
	SuccessIDs []int64                    `json:"success_ids"`
	FailedIDs  []int64                    `json:"failed_ids"`
	Results    []BulkUpdateProviderResult `json:"results"`
}

// ProviderBulkUpdate 表达批量配置补丁；nil 指针表示不修改对应字段。
type ProviderBulkUpdate struct {
	// ProtocolUpdates 是校验后的逐提供商非敏感协议补丁，同一 SQL 原子合并。
	ProtocolUpdates map[int64]map[string]any

	Name           *string
	ProxyID        *int64
	Concurrency    *int
	Priority       *int
	RateMultiplier *float64
	LoadFactor     *int
	Status         *string
	Schedulable    *bool
	Credentials    map[string]any
	Extra          map[string]any
	// EnsureCodexFingerprintSeed 要求仓储原子保留或生成启用收敛的提供商 seed。
	EnsureCodexFingerprintSeed bool
}
