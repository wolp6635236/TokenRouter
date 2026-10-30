package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// origins_test.go exercises author recognition independently of gateway support.
// Regional and product identities use verified lab relationships, while mixed
// catalogs retain author models and reject hosted third-party families even
// when the upstream catalog has omitted canonical links.

// TestClassifyModelOriginOfficialServices supplies author offerings through
// regional, coding, cloud, and independent-brand endpoints. Every verified
// relationship must grant author status without requiring a matching public record.
func TestClassifyModelOriginOfficialServices(t *testing.T) {
	tests := []struct {
		providers []string // equivalent official serving endpoints
		lab       string   // expected canonical author
		model     string   // authored model request identity
	}{
		{[]string{"minimax", "minimax-cn", "minimax-coding-plan", "minimax-cn-coding-plan"}, "minimax", "MiniMax-Hailuo-future"},
		{[]string{"volcengine", "volcengine-coding-plan", "byteplus"}, "bytedance-seed", "doubao-seedance-future"},
		{[]string{"alibaba", "alibaba-cn", "alibaba-coding-plan", "alibaba-coding-plan-cn", "alibaba-token-plan", "alibaba-token-plan-cn"}, "alibaba", "happyhorse-1.1-t2v"},
		{[]string{"modelscope"}, "alibaba", "Qwen/Qwen3-30B-A3B-Instruct-2507"},
		{[]string{"stepfun", "stepfun-ai", "stepfun-step-plan", "stepfun-ai-step-plan"}, "stepfun", "stepaudio-future"},
		{[]string{"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp"}, "xiaomi", "mimo-future"},
		{[]string{"moonshotai", "moonshotai-cn", "kimi-code-plan-cn", "kimi-code-plan-global"}, "moonshotai", "k-future"},
		{[]string{"zhipuai", "zai", "zhipuai-coding-plan", "zai-coding-plan"}, "zhipuai", "cogvideox-future"},
		{[]string{"tencent-coding-plan", "tencent-token-plan", "tencent-tokenhub"}, "tencent", "HunyuanVideo-future"},
		{[]string{"tencent-tokenhub"}, "tencent", "hy5-preview"},
		{[]string{"bailing"}, "inclusionai", "Ling-1T"},
		{[]string{"longcat"}, "meituan", "LongCat-2.0"},
		{[]string{"nova"}, "amazon", "nova-2-pro-v1"},
		{[]string{"llama"}, "meta", "llama-4-scout-17b-16e-instruct-fp8"},
		{[]string{"amazon-bedrock"}, "amazon", "us.amazon.nova-2-lite-v1:0"},
		{[]string{"arcee"}, "arcee-ai", "trinity-large-thinking"},
		{[]string{"watsonx"}, "ibm", "ibm/granite-4-h-small"},
		{[]string{"azure", "azure-cognitive-services", "github-copilot"}, "microsoft", "mai-code-1.1-flash"},
		{[]string{"fireworks-ai"}, "fireworks", "accounts/fireworks/models/ember-1"},
		{[]string{"pioneer"}, "fastino", "fastino/gliner2.5-base-v1"},
		{[]string{"nvidia"}, "nvidia", "nvidia/cosmos-transfer2_5-2b"},
		{[]string{"vispark"}, "vispark", "vispark/vision-small"},
		{[]string{"clarifai"}, "clarifai", "clarifai/main/models/mm-poly-8b"},
		{[]string{"morph"}, "morph", "morph-v3-large"},
		{[]string{"perplexity-agent"}, "perplexity", "sonar-pro"},
		{[]string{"sensenova"}, "sensenova", "sensenova-6.8-flash-lite"},
		{[]string{"vivgrid"}, "vivgrid", "viv-fast"},
		{[]string{"xpersona"}, "xpersona", "xpersona-frieren-coder"},
		{[]string{"abacus"}, "abacusai", "dracarys-llama-3.1-70b-instruct"},
		{[]string{"cloudflare-workers-ai"}, "cloudflare", "@cf/cloudflare/clef-flash"},
		{[]string{"inference"}, "inference-net", "schematron-v2-small"},
	}
	for _, test := range tests {
		for _, provider := range test.providers {
			t.Run(provider+"/"+test.model, func(t *testing.T) {
				input := Entry{Provider: provider, Model: test.model}
				result := classifyModelOrigin(input, nil)
				require.Equal(t, test.lab, catalogProviderOrigin(provider).lab)
				require.True(t, result.FirstParty)
				require.Equal(t, provider == test.lab, result.originalEndpoint)
				require.Equal(t, input.Model, result.Model)
				require.Empty(t, result.Canonical)
				input.Canonical = test.lab + "/future-family"
				input.Model = "unlisted-future-family"
				require.True(t, classifyModelOrigin(input, nil).FirstParty)
			})
		}
	}
	for _, provider := range []string{
		"ai21", "cohere", "meta", "perplexity", "poolside", "sakana", "sarvam", "thinkingmachines",
		"inception", "reflection", "agnes", "abliteration-ai", "lucidquery", "openai", "anthropic",
		"google", "xai", "deepseek", "mistral", "upstage", "typesafe",
	} {
		t.Run(provider+"/future", func(t *testing.T) {
			result := classifyModelOrigin(Entry{Provider: provider, Model: "future-native-model"}, nil)
			require.True(t, result.FirstParty)
		})
	}
}

// TestClassifyModelOriginRejectsHostedModels mixes unlinked foreign offerings,
// conflicting canonical authors, and misleading provider names. Verified author
// aliases must not promote these records into the first-party directory.
func TestClassifyModelOriginRejectsHostedModels(t *testing.T) {
	for _, entry := range []Entry{
		{Provider: "alibaba-cn", Model: "DeepSeek-R1"},
		{Provider: "alibaba-coding-plan", Model: "glm-5"},
		{Provider: "alibaba-token-plan", Model: "MiniMax-M2.5"},
		{Provider: "volcengine", Model: "deepseek-v4-flash-ga-260731"},
		{Provider: "volcengine-coding-plan", Model: "kimi-k3"},
		{Provider: "tencent-coding-plan", Model: "glm-5"},
		{Provider: "tencent-coding-plan", Model: "tc-code-latest"},
		{Provider: "fireworks-ai", Model: "accounts/fireworks/models/kimi-k3"},
		{Provider: "pioneer", Model: "openai/gpt-test"},
		{Provider: "watsonx", Model: "meta-llama/llama-4-maverick"},
		{Provider: "github-copilot", Model: "claude-opus-test"},
		{Provider: "azure", Model: "gpt-test"},
		{Provider: "arcee", Model: "deepseek/deepseek-test"},
		{Provider: "sensenova", Model: "kimi-k3"},
		{Provider: "clarifai", Model: "qwen/qwenLM/models/qwen-test"},
		{Provider: "morph", Model: "auto"},
		{Provider: "perplexity-agent", Model: "gpt-test"},
		{Provider: "xpersona", Model: "gpt-test"},
		{Provider: "cloudflare-workers-ai", Model: "@cf/meta/llama-test"},
		{Provider: "abacus", Model: "gpt-test"},
		{Provider: "inference", Model: "meta/llama-test"},
		{Provider: "minimax-relay", Model: "MiniMax-Hailuo-future", Canonical: "minimax/hailuo-future"},
		{Provider: "openrouter", Model: "doubao-seedance-future", Canonical: "bytedance-seed/seedance-future"},
		{Provider: "minimax-cn", Model: "MiniMax-test", Canonical: "otherlab/model"},
		{Provider: "volcengine", Model: "doubao-test", Canonical: "otherlab/model"},
		{Provider: "nvidia", Model: "nvidia/foreign-model", Canonical: "otherlab/model"},
		{Provider: "liquid", Model: "lfm2", Canonical: "liquid/lfm2"},
	} {
		t.Run(entry.Provider+"/"+entry.Model, func(t *testing.T) {
			require.False(t, classifyModelOrigin(entry, nil).FirstParty)
		})
	}
}

// TestClassifyModelOriginPublicEvidence supplies public facts for a previously
// unknown author and author-qualified NVIDIA IDs. Both spellings must retain
// public ownership, while an explicit conflicting declaration remains authoritative.
func TestClassifyModelOriginPublicEvidence(t *testing.T) {
	public := map[string]sourceModel{"newlab/model": {}, "nvidia/model": {}}
	for _, entry := range []Entry{
		{Provider: "newlab", Model: "model"},
		{Provider: "newlab", Model: "newlab/model"},
		{Provider: "nvidia", Model: "model"},
		{Provider: "nvidia", Model: "nvidia/model"},
	} {
		result := classifyModelOrigin(entry, public)
		require.True(t, result.FirstParty)
		require.Equal(t, entry.Provider+"/model", result.Canonical)
		entry.Canonical = "another/model"
		require.False(t, classifyModelOrigin(entry, public).FirstParty)
	}
}

// TestMatchesOriginPrefix checks versioned families, namespace suffixes, and
// similarly spelled unrelated names. Family recognition must accept future
// versions while preventing partial-name and empty-namespace matches.
func TestMatchesOriginPrefix(t *testing.T) {
	for _, test := range []struct {
		model  string // normalized candidate model
		prefix string // verified author family
		want   bool   // expected family match
	}{
		{"qwen4.0-future", "qwen", true},
		{"qwen/model", "qwen", true},
		{"qwenish-model", "qwen", false},
		{"hy5", "hy", true},
		{"hyper-other", "hy", false},
		{"nvidia/model", "nvidia/", true},
		{"nvidia/", "nvidia/", false},
		{"amazon.nova", "amazon.", true},
		{"amazon.", "amazon.", false},
	} {
		require.Equal(t, test.want, matchesOriginPrefix(test.model, test.prefix), test.model)
	}
}

// TestClassifyModelOriginSubscriptionIdentity enumerates ordinary and plan
// endpoints sharing author identities. Subscription classification must remain
// separate from first-party ownership so every author's records stay available.
func TestClassifyModelOriginSubscriptionIdentity(t *testing.T) {
	for subscription, providers := range map[bool][]string{
		true: {
			"kimi-code-plan-cn", "kimi-code-plan-global", "zai-coding-plan", "zhipuai-coding-plan",
			"minimax-coding-plan", "minimax-cn-coding-plan", "stepfun-step-plan", "stepfun-ai-step-plan",
			"xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp",
			"alibaba-coding-plan", "alibaba-coding-plan-cn", "alibaba-token-plan", "alibaba-token-plan-cn",
			"volcengine-coding-plan", "tencent-coding-plan", "tencent-token-plan",
		},
		false: {
			"moonshotai", "moonshotai-cn", "zai", "zhipuai", "minimax", "minimax-cn", "stepfun", "stepfun-ai",
			"xiaomi", "alibaba", "alibaba-cn", "modelscope", "volcengine", "byteplus", "tencent-tokenhub",
		},
	} {
		for _, provider := range providers {
			t.Run(provider, func(t *testing.T) {
				lab := catalogProviderOrigin(provider).lab
				entry := classifyModelOrigin(Entry{Provider: provider, Model: "future-model", Canonical: lab + "/future-model"}, nil)
				require.True(t, entry.FirstParty)
				require.Equal(t, subscription, entry.subscriptionEndpoint)
			})
		}
	}
}
