package modelcatalog

import "strings"

// origins.go attributes catalog offerings to their model authors across brands,
// regional endpoints, and subscription services. Provider IDs identify serving
// products and can differ from the lab IDs used by canonical metadata. The
// registry records those relationships independently of gateway platform
// support, so an author's new video, audio, image, or language model can enter
// the directory before a request adapter exists.
//
// Explicit canonical authorship takes precedence over every inferred match.
// Public metadata also establishes ownership for unlinked offerings. Dedicated
// author endpoints accept new unlinked model IDs, while mixed catalogs require
// verified author families or namespaces. These restrictions prevent a cloud
// provider's hosted third-party models from inheriting the cloud owner's status.
// Subscription service identity is recorded independently of authorship so
// bare lookups can prefer ordinary APIs while keeping plan records queryable.

// originRule identifies a catalog provider's author and its unlinked offerings.
type originRule struct {
	lab string // canonical model author
	// allowUnlinked and prefixes classify records lacking canonical and public
	// evidence. Dedicated author endpoints accept new identities; mixed catalogs
	// use verified model families or namespaces before granting author status.
	allowUnlinked bool     // unlinked author model acceptance
	prefixes      []string // verified author model prefixes
	// subscription marks plan-specific offerings whose bare aliases yield to
	// ordinary author APIs. Qualified lookups retain each service's full record.
	subscription bool
}

// classifyModelOrigin assigns authorship and inferred public identity to one
// offering. Explicit canonical ownership wins even when the endpoint or model
// name suggests a different author. Missing links first consult public metadata,
// including already author-qualified IDs, and then use the endpoint's rule.
//
// Provider brands and regional aliases resolve to their owning lab independently
// of runtime adapters. Subscription identity is recorded separately from
// authorship so ordinary APIs can precede plans during bare-name selection.
// Original-endpoint preference retains the provider-equals-lab rule among the
// selected service class. The returned entry shares attributes and prices with
// its input, and equally preferred offerings can still be ambiguous.
func classifyModelOrigin(entry Entry, public map[string]sourceModel) Entry {
	origin := catalogProviderOrigin(entry.Provider)
	if entry.Canonical != "" {
		lab, model, valid := strings.Cut(normalize(entry.Canonical), "/")
		entry.FirstParty = valid && model != "" && lab == origin.lab
	} else {
		canonical := origin.lab + "/" + entry.Model
		_, exists := public[canonical]
		// Author-qualified request IDs already contain the lab namespace. Match
		// their public identity directly so nested provider keys do not hide ownership.
		if !exists && strings.HasPrefix(normalize(entry.Model), origin.lab+"/") {
			canonical = entry.Model
			_, exists = public[canonical]
		}
		entry.FirstParty = exists || origin.allowUnlinked
		if exists {
			entry.Canonical = canonical
		}
		for _, prefix := range origin.prefixes {
			if matchesOriginPrefix(normalize(entry.Model), prefix) {
				entry.FirstParty = true
				break
			}
		}
	}
	entry.originalEndpoint = entry.FirstParty && entry.Provider == origin.lab
	entry.subscriptionEndpoint = origin.subscription
	return entry
}

// catalogProviderOrigin returns verified author relationships for serving
// products. Unknown IDs retain their own lab name and require canonical or
// public evidence. Prefix rules apply within the named provider, so a relay's
// matching model spelling alone cannot establish first-party ownership.
// @project-doc docs/interfaces/model_catalog_and_marketplace.md#model_catalog_origins
func catalogProviderOrigin(provider string) originRule {
	switch provider {
	case "openai", "anthropic", "google", "xai", "deepseek", "moonshotai", "mistral", "typesafe",
		"stepfun", "upstage", "ai21", "cohere", "meta", "minimax", "perplexity", "poolside",
		"sakana", "sarvam", "thinkingmachines", "xiaomi", "inception", "reflection",
		"agnes", "abliteration-ai", "lucidquery":
		return originRule{lab: provider, allowUnlinked: true}
	case "moonshotai-cn":
		return originRule{lab: "moonshotai", allowUnlinked: true}
	case "kimi-code-plan-cn", "kimi-code-plan-global":
		return originRule{lab: "moonshotai", allowUnlinked: true, subscription: true}
	case "zai", "zhipuai":
		return originRule{lab: "zhipuai", allowUnlinked: true}
	case "zai-coding-plan", "zhipuai-coding-plan":
		return originRule{lab: "zhipuai", allowUnlinked: true, subscription: true}
	case "minimax-cn":
		return originRule{lab: "minimax", allowUnlinked: true}
	case "minimax-coding-plan", "minimax-cn-coding-plan":
		return originRule{lab: "minimax", allowUnlinked: true, subscription: true}
	case "stepfun-ai":
		return originRule{lab: "stepfun", allowUnlinked: true}
	case "stepfun-step-plan", "stepfun-ai-step-plan":
		return originRule{lab: "stepfun", allowUnlinked: true, subscription: true}
	case "xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp":
		return originRule{lab: "xiaomi", allowUnlinked: true, subscription: true}
	case "bailing":
		return originRule{lab: "inclusionai", allowUnlinked: true}
	case "longcat":
		return originRule{lab: "meituan", allowUnlinked: true}
	case "nova":
		return originRule{lab: "amazon", allowUnlinked: true}
	case "llama":
		return originRule{lab: "meta", allowUnlinked: true}
	case "alibaba", "alibaba-cn", "alibaba-coding-plan", "alibaba-coding-plan-cn",
		"alibaba-token-plan", "alibaba-token-plan-cn", "modelscope":
		return originRule{
			lab:          "alibaba",
			subscription: provider != "alibaba" && provider != "alibaba-cn" && provider != "modelscope",
			prefixes: []string{
				"alibaba/", "qwen", "qwq", "qvq", "wan", "happyhorse", "happyoyster", "tongyi",
				"text-embedding", "gte", "paraformer", "cosyvoice", "sambert", "funasr", "fun-asr",
			},
		}
	case "volcengine", "volcengine-coding-plan", "byteplus":
		return originRule{
			lab:          "bytedance-seed",
			subscription: provider == "volcengine-coding-plan",
			prefixes: []string{
				"bytedance/", "bytedance-seed/", "doubao", "seed", "seedance", "seedream", "dreamina", "omnihuman",
			},
		}
	case "tencent-coding-plan", "tencent-token-plan", "tencent-tokenhub":
		return originRule{
			lab:          "tencent",
			subscription: provider != "tencent-tokenhub",
			prefixes:     []string{"tencent/", "hunyuan", "hunyuanvideo", "hy"},
		}
	case "google-vertex", "google-vertex-anthropic":
		return originRule{lab: "google", prefixes: []string{"google/", "gemini", "gemma", "imagen", "veo", "lyria"}}
	case "arcee":
		return originRule{lab: "arcee-ai", prefixes: []string{"arcee-ai/", "arcee", "trinity"}}
	case "watsonx":
		return originRule{lab: "ibm", prefixes: []string{"ibm/", "ibm-granite/", "granite"}}
	case "azure", "azure-cognitive-services", "github-copilot":
		return originRule{lab: "microsoft", prefixes: []string{"microsoft/", "mai", "phi"}}
	case "amazon-bedrock":
		return originRule{lab: "amazon", prefixes: []string{
			"amazon.", "us.amazon.", "eu.amazon.", "apac.amazon.", "global.amazon.",
		}}
	case "fireworks-ai":
		return originRule{lab: "fireworks", prefixes: []string{"accounts/fireworks/models/ember", "fireworks/ember", "ember"}}
	case "pioneer":
		return originRule{lab: "fastino", prefixes: []string{"fastino/"}}
	case "nvidia", "vispark", "clarifai":
		return originRule{lab: provider, prefixes: []string{provider + "/"}}
	case "sensenova":
		return originRule{lab: provider, prefixes: []string{"sensenova"}}
	case "morph":
		return originRule{lab: provider, prefixes: []string{"morph"}}
	case "perplexity-agent":
		return originRule{lab: "perplexity", prefixes: []string{"perplexity/", "sonar"}}
	case "vivgrid":
		return originRule{lab: provider, prefixes: []string{"viv"}}
	case "xpersona":
		return originRule{lab: provider, prefixes: []string{"xpersona"}}
	case "abacus":
		return originRule{lab: "abacusai", prefixes: []string{"abacusai/", "dracarys"}}
	case "cloudflare-workers-ai":
		return originRule{lab: "cloudflare", prefixes: []string{"@cf/cloudflare/", "cloudflare/"}}
	case "inference":
		return originRule{lab: "inference-net", prefixes: []string{"inference-net/", "schematron"}}
	case "liquid":
		// This provider is the Liquid Inference exchange, whose product identity
		// differs from Liquid AI's public lab. Its router cannot inherit LFM ownership.
		return originRule{lab: "liquid-inference"}
	default:
		return originRule{lab: provider}
	}
}

// matchesOriginPrefix matches normalized author namespaces or versioned model
// families. Namespace prefixes end with slash or dot and require a suffix;
// family names accept separators and numeric versions, preventing similarly
// spelled unrelated names from acquiring ownership.
func matchesOriginPrefix(model, prefix string) bool {
	if !strings.HasPrefix(model, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(model, prefix)
	if strings.HasSuffix(prefix, "/") || strings.HasSuffix(prefix, ".") {
		return suffix != ""
	}
	if suffix == "" {
		return true
	}
	return strings.ContainsRune("-_/.:0123456789", rune(suffix[0]))
}
