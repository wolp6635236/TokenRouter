package httpapi

// newOpenAIWSV2TestConfig 为网络夹具设置短退避，算法测试自行指定时长。
func newOpenAIWSV2TestConfig() *wsFixtureOptions {
	options := &wsFixtureOptions{}
	options.WS.Enabled = true
	options.WS.OAuthEnabled = true
	options.WS.APIKeyEnabled = true
	options.WS.ResponsesWebsocketsV2 = true
	options.WS.StickyResponseIDTTLSeconds = 3600
	options.WS.RetryBackoffInitialMS = 1
	options.WS.RetryBackoffMaxMS = 1
	return options
}
