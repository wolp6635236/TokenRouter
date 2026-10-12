package provider

const (
	ProviderListGroupUngrouped     int64 = -1
	ProviderPrivacyModeUnsetFilter       = "__unset__"
)

// OAuthRefreshPageOptions 描述一次有界且游标稳定的 OAuth 提供商扫描。
// 候选平台来自 TokenRefreshService 注册表，与已登记的刷新器对应。
type OAuthRefreshPageOptions struct {
	Platforms            []string
	AfterID              int64
	Limit                int
	ActiveOnly           bool
	IncludeSetupToken    bool
	RequireRefreshToken  bool
	ExcludeRetryCooldown bool
}

// OAuthRefreshCandidatePage 保存 SQL 查询页面的 ID 游标元数据。
// 详情加载时记录被并发删除后，调用方仍通过原始页面的游标继续扫描。
type OAuthRefreshCandidatePage struct {
	Providers   []Record
	NextAfterID int64
	HasMore     bool
}
