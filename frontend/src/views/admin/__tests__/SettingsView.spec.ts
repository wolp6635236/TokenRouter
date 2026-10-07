import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import { flushPromises, mount, DOMWrapper } from "@vue/test-utils";

import SettingsView from "../SettingsView.vue";
import BaseDialog from "@/components/common/BaseDialog.vue";

const {
  getSettings,
  getCreativeModelCandidates,
  getCreativeWorkerStatus,
  updateSettings,
  getWebSearchEmulationConfig,
  updateWebSearchEmulationConfig,
  testWebSearchEmulation,
  getAdminApiKey,
  getOverloadCooldownSettings,
  getOpenAI403CooldownSettings,
  updateOpenAI403CooldownSettings,
  getRateLimit429CooldownSettings,
  updateRateLimit429CooldownSettings,
  getPanelRateLimitSettings,
  updatePanelRateLimitSettings,
  getPreAggregationSettings,
  updatePreAggregationSettings,
  backfillPreAggregation,
  getStreamTimeoutSettings,
  getRectifierSettings,
  getBetaPolicySettings,
  getOllamaCloudUsageSettings,
  updateOllamaCloudUsageSettings,
  getGroups,
  listProxies,
  getProviders,
  updateProvider,
  createProvider,
  deleteProvider,
  fetchPublicSettings,
  adminSettingsFetch,
  showError,
  showSuccess,
  getMarketplaceModels,
  getMarketplaceStats,
} = vi.hoisted(() => ({
  getSettings: vi.fn(),
  getCreativeModelCandidates: vi.fn(),
  getCreativeWorkerStatus: vi.fn(),
  updateSettings: vi.fn(),
  getWebSearchEmulationConfig: vi.fn(),
  updateWebSearchEmulationConfig: vi.fn(),
  testWebSearchEmulation: vi.fn(),
  getAdminApiKey: vi.fn(),
  getOverloadCooldownSettings: vi.fn(),
  getOpenAI403CooldownSettings: vi.fn(),
  updateOpenAI403CooldownSettings: vi.fn(),
  getRateLimit429CooldownSettings: vi.fn(),
  updateRateLimit429CooldownSettings: vi.fn(),
  getPanelRateLimitSettings: vi.fn().mockResolvedValue({
    enabled: true,
    user_rpm: 240,
    heavy_rpm: 60,
    exempt_admin: true,
    public_ip_rpm: 300,
  }),
  updatePanelRateLimitSettings: vi.fn().mockImplementation(async (payload) => payload),
  getPreAggregationSettings: vi.fn().mockResolvedValue({
    settings: {
      usage: { enabled: true, interval_seconds: 60 },
      ops: { enabled: true },
    },
    availability: {
      usage_available: true,
      ops_available: true,
      manual_backfill_available: true,
      manual_backfill_max_days: 31,
    },
    usage_status: { phase: "idle", lag_seconds: 0, last_duration_ms: 0 },
    ops_status: { phase: "idle", lag_seconds: 0, last_duration_ms: 0 },
  }),
  updatePreAggregationSettings: vi.fn(),
  backfillPreAggregation: vi.fn(),
  getStreamTimeoutSettings: vi.fn(),
  getRectifierSettings: vi.fn(),
  getBetaPolicySettings: vi.fn(),
  getOllamaCloudUsageSettings: vi.fn().mockResolvedValue({
    enabled: false,
    interval_minutes: 60,
    debounce_minutes: 1,
  }),
  updateOllamaCloudUsageSettings: vi.fn().mockImplementation(async (payload) => payload),
  getGroups: vi.fn(),
  listProxies: vi.fn(),
  getProviders: vi.fn(),
  updateProvider: vi.fn(),
  createProvider: vi.fn(),
  deleteProvider: vi.fn(),
  fetchPublicSettings: vi.fn(),
  adminSettingsFetch: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  getMarketplaceModels: vi.fn().mockResolvedValue([]),
  getMarketplaceStats: vi.fn().mockResolvedValue({
    today_tokens: 0,
    total_tokens: 0,
    total_users: 0,
  }),
}));

// 使用 Vue ref，使模板取得语言字符串。
const localeRef = await vi.hoisted(async () => {
  const { ref } = await import("vue");
  return ref("zh-CN");
});

vi.mock("@/api", () => ({
  adminAPI: {
    settings: {
      getSettings,
      getCreativeModelCandidates,
      getCreativeWorkerStatus,
      updateSettings,
      getWebSearchEmulationConfig,
      updateWebSearchEmulationConfig,
      testWebSearchEmulation,
      getAdminApiKey,
      getOverloadCooldownSettings,
      getOpenAI403CooldownSettings,
      updateOpenAI403CooldownSettings,
      getRateLimit429CooldownSettings,
      updateRateLimit429CooldownSettings,
      getPanelRateLimitSettings,
      updatePanelRateLimitSettings,
      getPreAggregationSettings,
      updatePreAggregationSettings,
      backfillPreAggregation,
      getStreamTimeoutSettings,
      getRectifierSettings,
      getBetaPolicySettings,
    },
    providers: {
      getOllamaCloudUsageSettings,
      updateOllamaCloudUsageSettings,
    },
    groups: {
      getAll: getGroups,
    },
    proxies: {
      list: listProxies,
    },
    payment: {
      getProviders,
      updateProvider,
      createProvider,
      deleteProvider,
    },
  },
}));

vi.mock("@/api/marketplace", () => ({
  getMarketplaceModels,
  getMarketplaceStats,
}));

vi.mock("@/stores", () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning: vi.fn(),
    showInfo: vi.fn(),
    fetchPublicSettings,
  }),
}));

vi.mock("@/stores/adminSettings", () => ({
  useAdminSettingsStore: () => ({
    fetch: adminSettingsFetch,
  }),
}));

vi.mock("@/composables/useClipboard", () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn(),
  }),
}));

vi.mock("@/utils/apiError", () => ({
  extractApiErrorMessage: () => "error",
}));

vi.mock("@/composables/useBalanceDisplay", () => ({
  useBalanceDisplay: () => ({
    balanceUnitName: { value: "USD" },
    balanceUnitSymbol: { value: "$" },
    usdUnitName: "USD",
    usdUnitSymbol: "$",
    balanceIconSvg: { value: "" },
    hasCustomBalanceIcon: { value: false },
    formatBalanceAmount: (value: number | null | undefined) => `$${Number(value ?? 0).toFixed(2)}`,
    formatUsdAmount: (value: number | null | undefined) => `$${Number(value ?? 0).toFixed(2)}`,
  }),
}));

vi.mock("vue-router", () => ({
  useRoute: () => ({ query: {}, hash: "" }),
}));

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  const translations: Record<string, string> = {
    "admin.settings.registration.emailNormalization": "邮箱地址归一化",
    "admin.settings.registration.emailNormalizationHint":
      "启用后，注册或修改邮箱时会按归一化后的邮箱地址检查重复注册。",
    "admin.settings.registration.emailDomainQuota": "非白名单域名限量注册",
    "admin.settings.registration.emailDomainQuotaHint":
      "开启后，其他可注册主域名各限注册一个提供商。",
    "admin.settings.wechatConnect.title": "微信登录",
    "admin.settings.wechatConnect.description": "用于微信开放平台或公众号/小程序的第三方登录配置。",
    "admin.settings.wechatConnect.enabledLabel": "启用微信登录",
    "admin.settings.wechatConnect.enabledHint": "开启后可使用微信第三方登录回调与授权配置。",
    "admin.settings.wechatConnect.appIdLabel": "AppID",
    "admin.settings.wechatConnect.appIdPlaceholder": "微信开放平台 AppID",
    "admin.settings.wechatConnect.appSecretLabel": "AppSecret",
    "admin.settings.wechatConnect.appSecretConfiguredPlaceholder": "密钥已配置，留空以保留当前值。",
    "admin.settings.wechatConnect.appSecretPlaceholder": "微信开放平台 AppSecret",
    "admin.settings.wechatConnect.appSecretConfiguredHint": "密钥已配置，留空以保留当前值。",
    "admin.settings.wechatConnect.appSecretHint": "填写后会覆盖当前微信密钥。",
    "admin.settings.wechatConnect.modeLabel": "模式",
    "admin.settings.wechatConnect.openModeLabel": "非微信环境使用开放平台",
    "admin.settings.wechatConnect.openModeHint": "浏览器不在微信内时，自动走开放平台扫码授权。",
    "admin.settings.wechatConnect.mpModeLabel": "微信环境使用公众号",
    "admin.settings.wechatConnect.mpModeHint": "浏览器在微信内时，自动走公众号授权。",
    "admin.settings.wechatConnect.redirectUrlLabel": "回调地址",
    "admin.settings.wechatConnect.redirectUrlPlaceholder": "https://your-site.com/api/v1/auth/oauth/wechat/callback",
    "admin.settings.wechatConnect.generateAndCopy": "使用当前站点生成并复制",
    "admin.settings.wechatConnect.redirectUrlSetAndCopied": "已使用当前站点生成回调地址并复制到剪贴板",
    "admin.settings.wechatConnect.frontendRedirectUrlLabel": "前端回调地址",
    "admin.settings.wechatConnect.frontendRedirectUrlPlaceholder": "/auth/wechat/callback",
    "admin.settings.wechatConnect.frontendRedirectUrlHint": "通常用于前端路由回调地址，需与后端配置保持一致。",
    "admin.settings.authSourceDefaults.title": "认证来源默认值",
    "admin.settings.authSourceDefaults.description": "按注册来源配置新用户默认余额、并发、订阅与授权策略。",
    "admin.settings.authSourceDefaults.requireEmailLabel": "第三方注册强制补充邮箱",
    "admin.settings.authSourceDefaults.requireEmailHint": "启用后，Linux DO、OIDC、微信注册缺少邮箱时必须先补充邮箱地址。",
    "admin.settings.authSourceDefaults.enabledHint": "以下默认值会在该来源注册新用户时发放；首次绑定时授权仅作用于已有提供商绑定该来源。",
    "admin.settings.authSourceDefaults.sources.email.title": "邮箱注册",
    "admin.settings.authSourceDefaults.sources.email.description": "适用于邮箱密码注册的新用户默认配额。",
    "admin.settings.authSourceDefaults.sources.linuxdo.title": "Linux DO 登录",
    "admin.settings.authSourceDefaults.sources.linuxdo.description": "适用于 Linux DO 第三方注册的新用户默认配额。",
    "admin.settings.authSourceDefaults.sources.oidc.title": "OIDC 登录",
    "admin.settings.authSourceDefaults.sources.oidc.description": "适用于 OIDC 第三方注册的新用户默认配额。",
    "admin.settings.authSourceDefaults.sources.wechat.title": "微信登录",
    "admin.settings.authSourceDefaults.sources.wechat.description": "适用于微信第三方注册的新用户默认配额。",
    "admin.settings.authSourceDefaults.grantOnFirstBindLabel": "首次绑定时授权",
    "admin.settings.authSourceDefaults.grantOnFirstBindHint": "已有提供商首次绑定该来源时发放默认权益。",
    "admin.settings.authSourceDefaults.defaultSubscriptionsLabel": "默认订阅",
    "admin.settings.authSourceDefaults.defaultSubscriptionsHint": "仅对当前认证来源生效，未配置时不追加来源专属订阅。",
    "admin.settings.authSourceDefaults.noSourceSubscriptions": "当前来源未配置专属默认订阅。",
    "admin.settings.paymentVisibleMethods.methodLabel": "{title} 可见方式",
    "admin.settings.paymentVisibleMethods.methodHint": "控制前台结算页是否展示该方式，以及展示时使用的来源键。",
    "admin.settings.paymentVisibleMethods.sourceLabel": "支付来源",
    "admin.settings.paymentVisibleMethods.sourceHint": "启用后必须明确选择一个来源；未配置状态不会对外展示该支付方式。",
    "admin.settings.paymentVisibleMethods.sourceRequiredError": "{title} 已启用，请先选择支付来源。",
    "admin.settings.payment.configGuide": "查看支付配置说明",
    "admin.settings.gatewaySections.label": "网关设置分类",
    "admin.settings.gatewaySections.general": "通用",
    "admin.settings.gatewaySections.anthropic": "Anthropic",
    "admin.settings.gatewaySections.openai": "OpenAI",
    "admin.settings.gatewaySections.grok": "Grok",
    "admin.settings.gatewaySections.antigravity": "Antigravity",
    "admin.settings.gatewaySections.ollamaCloud": "Ollama Cloud",
    "admin.settings.scheduling.advancedTitle": "通用高级调度器",
    "admin.settings.scheduling.advancedDescription": "高级调度器按分组启用，使用通用候选评分、Top-K 加权和运行时错误率与首 token 延迟反馈。未启用高级调度器的分组继续使用基础调度器。",
    "admin.settings.scheduling.advancedHelp.trigger": "查看高级调度器评分与选择原理",
    "admin.settings.scheduling.advancedHelp.title": "高级调度器如何工作",
    "admin.settings.scheduling.advancedHelp.summary": "请求会先通过硬过滤，再对剩余候选提供商打分；高分提供商进入 Top-K 候选池。",
    "admin.settings.scheduling.advancedHelp.formula": "总分 = 各信号的归一化得分 × 对应权重 + 可选的粘性加分",
    "admin.settings.scheduling.advancedHelp.hardFilterTitle": "先硬过滤：",
    "admin.settings.scheduling.advancedHelp.hardFilter": "按分组、模型与能力、提供商状态、限流、代理和并发槽位排除不可用提供商。",
    "admin.settings.scheduling.advancedHelp.scoreTitle": "再计算分数：",
    "admin.settings.scheduling.advancedHelp.score": "优先级、低负载、低排队、低错误率、低首 token 延迟、窗口重置和额度余量会按权重加总；缺失的可选信号保持中性。",
    "admin.settings.scheduling.advancedHelp.feedbackTitle": "持续反馈：",
    "admin.settings.scheduling.advancedHelp.feedback": "请求结果和首 token 延迟以平滑统计更新提供商表现，让后续请求避开近期表现较差的提供商。",
    "admin.settings.scheduling.advancedHelp.selectionTitle": "Top-K 加权选择：",
    "admin.settings.scheduling.advancedHelp.selection": "取分数最高的 Top-K，再按分数权重随机选择，避免单一提供商长期垄断；开启粘性加权后，上一响应和会话提供商会获得额外优先。",
    "admin.settings.scheduling.advancedHelp.boundary": "每次尝试都会重查硬约束；发生分组回退时按目标分组重新调度。响应流开始后不会中途切换提供商。",
    "admin.settings.openaiQuotaAutoPause.title": "OpenAI 提供商配额自动暂停",
    "admin.settings.openaiQuotaAutoPause.description": "当 OpenAI 提供商 5h / 7d 用量达到阈值时，调度会自动跳过该提供商；窗口滚动后自动恢复。提供商级阈值优先于此全局默认值。",
    "admin.settings.openaiQuotaAutoPause.default5h": "默认 5h 用量阈值 (%)",
    "admin.settings.openaiQuotaAutoPause.default7d": "默认 7d 用量阈值 (%)",
    "admin.settings.openaiQuotaAutoPause.thresholdHint": "取值 0-100，留空或 0 表示不启用全局默认阈值。",
    "admin.settings.openaiQuotaAutoPause.rangeError": "OpenAI 配额自动暂停阈值必须在 0-100 之间",
    "admin.settings.scheduling.stickyWeightedTitle": "粘性加权",
    "admin.settings.scheduling.stickyWeightedDescription": "开启后，上一响应和会话粘性作为评分信号参与选择；关闭时保留既有粘性优先行为。",
    "admin.settings.scheduling.subscriptionPriorityTitle": "订阅优先",
    "admin.settings.scheduling.subscriptionPriorityDescription": "具备订阅能力的提供商会获得订阅优先评分；缺少该能力的提供商保持中性。",
    "admin.settings.scheduling.stickyEscapeTitle": "粘性健康切换",
    "admin.settings.scheduling.stickyEscapeDescription": "绑定提供商的错误率或首 token 延迟超过阈值时，本次请求允许切换到其它提供商，并保留原粘性绑定。",
    "admin.settings.scheduling.stickyEscapeEnabled": "强制粘性切换",
    "admin.settings.scheduling.stickyEscapeTTFT": "TTFT 切换阈值（毫秒）",
    "admin.settings.scheduling.stickyEscapeErrorRate": "错误率切换阈值（0-1）",
    "admin.settings.scheduling.ewmaTitle": "EWMA 反馈因子",
    "admin.settings.scheduling.ewmaDescription": "控制运行时错误率和首 token 延迟反馈对最新样本的敏感度；值越大越重视最新观测。",
    "admin.settings.scheduling.ewmaErrorRateAlpha": "错误率 EWMA 因子",
    "admin.settings.scheduling.ewmaTTFTAlpha": "TTFT EWMA 因子",
    "admin.settings.scheduling.weightsTitle": "高级调度权重",
    "admin.settings.scheduling.weightsDescription": "留空时使用配置或内置默认值；非空设置优先。权重对所有启用高级调度器的分组生效。",
    "admin.settings.scheduling.defaultPlaceholder": "配置/默认：{value}",
    "admin.settings.scheduling.topKLabel": "Top-K",
    "admin.settings.scheduling.priorityWeight": "优先级",
    "admin.settings.scheduling.loadWeight": "负载",
    "admin.settings.scheduling.queueWeight": "排队",
    "admin.settings.scheduling.errorRateWeight": "错误率",
    "admin.settings.scheduling.ttftWeight": "首 token 延迟",
    "admin.settings.scheduling.resetWeight": "窗口重置",
    "admin.settings.scheduling.quotaHeadroomWeight": "额度余量",
    "admin.settings.scheduling.previousResponseWeight": "上一响应粘性",
    "admin.settings.scheduling.sessionStickyWeight": "会话粘性",
    "admin.settings.openaiFastPolicy.summaryTargetModels": "目标模型",
    "admin.settings.openaiFastPolicy.summaryAllModels": "全部模型",
    "admin.settings.openaiFastPolicy.summaryOtherModels": "其他模型",
    "admin.settings.openaiFastPolicy.summaryAction.filter": "过滤",
    "admin.settings.openaiFastPolicy.summaryAction.pass": "透传",
    "admin.settings.site.uploadImage": "上传图片",
    "admin.settings.site.remove": "移除",
    "admin.settings.platformQuota.platform": "平台",
    "admin.settings.platformQuota.daily": "日限额 (USD)",
    "admin.settings.platformQuota.weekly": "周限额 (USD)",
    "admin.settings.platformQuota.monthly": "月限额 (USD, 30天滚动)",
    "admin.settings.platformQuota.placeholder": "不限",
    "admin.settings.defaults.defaultPlatformQuotas": "默认平台限额（注册时分配）",
    "admin.settings.defaults.defaultPlatformQuotasHint": "新用户注册时自动写入平台限额记录；已有用户不受影响。留空 = 该平台该窗口不限制。",
    "admin.settings.defaults.platformQuotaNotice": "月限额为 30 天滚动窗口，非自然月",
    "admin.settings.authSourceDefaults.platformQuotasOverride": "平台限额覆盖",
    "admin.settings.authSourceDefaults.platformQuotasOverrideHint": "留空的字段继承「系统默认平台限额」；填 0 表示禁止该窗口使用。",
    "admin.providers.openai.oauthPassthrough": "自动透传（仅替换认证）",
    "admin.providers.openai.oauthPassthroughDesc": "开启后，该 OpenAI 提供商将自动透传请求与响应，仅替换认证并保留计费/并发/审计及必要安全过滤；如遇兼容性问题可随时关闭回滚。",
    "admin.providers.openai.wsMode": "WS mode",
    "admin.providers.openai.wsModeDesc": "仅对当前 OpenAI 提供商类型生效。",
    "admin.providers.openai.wsModeOff": "关闭（off）",
    "admin.providers.openai.wsModeCtxPool": "上下文池（ctx_pool）",
    "admin.providers.openai.wsModePassthrough": "透传（passthrough）",
    "admin.providers.openai.codexCLIOnly": "仅允许 Codex 官方客户端",
    "admin.providers.openai.codexCLIOnlyDesc": "仅对 OpenAI OAuth 生效。开启后仅允许 Codex 官方客户端家族访问；关闭后完全绕过并保持原逻辑。",
    "admin.providers.openai.codexCLIOnlyAllowClaudeCode": "额外放行 Claude Code 的 Codex 插件",
    "admin.providers.openai.codexCLIOnlyAllowClaudeCodeDesc": "仅在上方开关开启时生效。额外放行通过 Claude Code 的 Codex 插件发起的请求。",
    "admin.providers.autoPause5hDisabled": "禁用 5h 自动暂停",
    "admin.providers.autoPause7dDisabled": "禁用 7d 自动暂停",
    "admin.providers.autoPause5hThreshold": "5h 用量阈值(%)",
    "admin.providers.autoPause7dThreshold": "7d 用量阈值(%)",
    "admin.providers.autoPauseDisabledHint": "开启后该提供商永不进入自动暂停。",
    "admin.providers.autoPauseThresholdHint": "留空或填 0 表示使用全局默认阈值。",
    "admin.providers.openai.compactMode": "旧版 Compact 端点",
    "admin.providers.openai.compactModeDesc": "仅控制本提供商参与旧版 /responses/compact 调度，不影响原生 remote_compaction_v2。自动跟随探测结果，强制开启始终允许，强制关闭始终排除。",
    "admin.providers.openai.compactModeAuto": "自动",
    "admin.providers.openai.compactModeForceOn": "强制开启",
    "admin.providers.openai.compactModeForceOff": "强制关闭",
    "admin.providers.openai.nativeCompactV2Mode": "原生 V2 压缩",
    "admin.providers.openai.nativeCompactV2ModeDesc": "仅控制本提供商参与原生 remote_compaction_v2 调度。自动跟随原生 V2 探测结果，强制开启始终允许，强制关闭始终排除。",
    "admin.providers.openai.nativeCompactV2ModeAuto": "自动",
    "admin.providers.openai.nativeCompactV2ModeForceOn": "强制开启",
    "admin.providers.openai.nativeCompactV2ModeForceOff": "强制关闭",
    "admin.providers.quotaControl.tlsFingerprint.label": "TLS 指纹模拟",
    "admin.providers.quotaControl.tlsFingerprint.hint": "模拟 Node.js/Claude Code/Codex CLI 客户端的 TLS 指纹",
    "admin.providers.quotaControl.tlsFingerprint.defaultProfile": "内置默认",
    "admin.providers.quotaControl.tlsFingerprint.randomProfile": "随机",
    "admin.providers.modelWhitelist": "模型白名单",
    "admin.providers.modelMapping": "模型映射",
    "admin.providers.mapRequestModels": "将请求模型映射到实际模型。",
    "admin.providers.addMapping": "添加映射",
  };
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) =>
        (translations[key] ?? key).replace(/\{(\w+)\}/g, (_, token) => params?.[token] ?? `{${token}}`),
      locale: localeRef,
    }),
  };
});

const AppLayoutStub = { template: "<div><slot /></div>" };
const ToggleStub = defineComponent({
  props: {
    modelValue: {
      type: Boolean,
      default: false,
    },
    size: {
      type: String,
      default: "md",
    },
    disabled: {
      type: Boolean,
      default: false,
    },
  },
  emits: ["update:modelValue"],
  inheritAttrs: false,
  setup(props, { attrs, emit }) {
    return () =>
      h("input", {
        ...attrs,
        class: "toggle-stub",
        type: "checkbox",
        "data-testid": attrs["data-testid"],
        checked: props.modelValue,
        disabled: props.disabled,
        onChange: (event: Event) => {
          emit("update:modelValue", (event.target as HTMLInputElement).checked);
        },
      });
  },
});

const SelectStub = defineComponent({
  props: {
    modelValue: {
      type: [String, Number, Boolean, null],
      default: "",
    },
    options: {
      type: Array,
      default: () => [],
    },
    placeholder: {
      type: String,
      default: "",
    },
  },
  emits: ["update:modelValue", "change"],
  setup(props, { emit }) {
    const onChange = (event: Event) => {
      const target = event.target as HTMLSelectElement;
      const option =
        (props.options as Array<Record<string, unknown>>).find(
          (item) => String(item.value ?? "") === target.value,
        ) ?? null;
      const value = option
        ? (option.value as string | number | boolean | null)
        : target.value;
      emit("update:modelValue", value);
      emit("change", value, option);
    };

    return () =>
      h(
        "select",
        {
          class: "select-stub",
          value: props.modelValue ?? "",
          "data-placeholder": props.placeholder,
          onChange,
        },
        (props.options as Array<Record<string, unknown>>).map((option) =>
          h(
            "option",
            {
              key: `${String(option.value ?? "")}:${String(option.label ?? "")}`,
              value: option.value as string,
            },
            String(option.label ?? ""),
          ),
        ),
      );
  },
});

const ImageUploadStub = defineComponent({
  props: {
    modelValue: {
      type: String,
      default: "",
    },
    uploadLabel: {
      type: String,
      default: "",
    },
    removeLabel: {
      type: String,
      default: "",
    },
    placeholder: {
      type: String,
      default: "",
    },
  },
  setup(props) {
    return () =>
      h("div", {
        class: "image-upload-stub",
        "data-model-value": props.modelValue,
        "data-upload-label": props.uploadLabel,
        "data-remove-label": props.removeLabel,
        "data-placeholder": props.placeholder,
      });
  },
});

const baseSettingsResponse = {
  registration_enabled: true,
  email_verify_enabled: false,
  registration_email_suffix_whitelist: [],
  registration_email_normalization: false,
  registration_email_domain_quota_enabled: false,
  user_email_change_enabled: false,
  promo_code_enabled: true,
  invitation_code_enabled: false,
  password_reset_enabled: false,
  totp_enabled: false,
  totp_encryption_key_configured: false,
  default_balance: 0,
  default_concurrency: 1,
  default_user_api_key_limit: 100,
  default_subscriptions: [],
  site_name: "TokenRouter",
  site_logo: "",
  site_subtitle: "",
  site_name_zh: "",
  site_name_en: "",
  site_title_zh: "",
  site_title_en: "",
  site_subtitle_zh: "",
  site_subtitle_en: "",
  api_base_url: "",
  contact_info: "",
  doc_url: "",
  home_content: "",
  hide_ccs_import_button: false,
  table_default_page_size: 20,
  table_page_size_options: [10, 20, 50, 100],
  backend_mode_enabled: false,
  custom_menu_items: [],
  custom_endpoints: [],
  frontend_url: "",
  smtp_host: "",
  smtp_port: 587,
  smtp_username: "",
  smtp_password_configured: false,
  smtp_from_email: "",
  smtp_from_name: "",
  smtp_use_tls: true,
  turnstile_enabled: false,
  turnstile_site_key: "",
  turnstile_secret_key_configured: false,
  tencent_captcha_enabled: false,
  tencent_captcha_app_id: "",
  tencent_captcha_app_secret_key_configured: false,
  tencent_captcha_cloud_secret_id_configured: false,
  tencent_captcha_cloud_secret_key_configured: false,
  api_key_acl_trust_forwarded_ip: true,
  forwarded_client_ip_headers: [],
  linuxdo_connect_enabled: false,
  linuxdo_connect_client_id: "",
  linuxdo_connect_client_secret_configured: false,
  linuxdo_connect_redirect_url: "",
  wechat_connect_enabled: true,
  wechat_connect_app_id: "wx-app-id-123",
  wechat_connect_app_secret_configured: true,
  wechat_connect_open_enabled: false,
  wechat_connect_mp_enabled: true,
  wechat_connect_mode: "mp",
  wechat_connect_scopes: "",
  wechat_connect_redirect_url:
    "https://admin.example.com/api/v1/auth/oauth/wechat/callback",
  wechat_connect_frontend_redirect_url: "/auth/wechat/callback",
  oidc_connect_enabled: false,
  oidc_connect_provider_name: "OIDC",
  oidc_connect_client_id: "",
  oidc_connect_client_secret_configured: false,
  oidc_connect_issuer_url: "",
  oidc_connect_discovery_url: "",
  oidc_connect_authorize_url: "",
  oidc_connect_token_url: "",
  oidc_connect_userinfo_url: "",
  oidc_connect_jwks_url: "",
  oidc_connect_scopes: "openid email profile",
  oidc_connect_redirect_url: "",
  oidc_connect_frontend_redirect_url: "/auth/oidc/callback",
  oidc_connect_token_auth_method: "client_secret_post",
  oidc_connect_use_pkce: true,
  oidc_connect_validate_id_token: true,
  oidc_connect_allowed_signing_algs: "RS256,ES256,PS256",
  oidc_connect_clock_skew_seconds: 120,
  oidc_connect_require_email_verified: false,
  oidc_connect_userinfo_email_path: "",
  oidc_connect_userinfo_id_path: "",
  oidc_connect_userinfo_username_path: "",
  enable_model_fallback: false,
  fallback_model_anthropic: "",
  fallback_model_openai: "",
  fallback_model_gemini: "",
  fallback_model_antigravity: "",
  grok_default_text_model: "grok-4.5",
  grok_cross_client_model_map_enabled: false,
  enable_identity_patch: false,
  identity_patch_prompt: "",
  ops_monitoring_enabled: false,
  ops_realtime_monitoring_enabled: false,
  ops_metrics_interval_seconds: 60,
  min_claude_code_version: "",
  max_claude_code_version: "",
  allow_ungrouped_key_scheduling: false,
  enable_fingerprint_unification: true,
  enable_metadata_passthrough: false,
  enable_cch_signing: false,
  enable_claude_oauth_system_prompt_injection: true,
  claude_oauth_system_prompt: "",
  claude_oauth_system_prompt_blocks: "",
  enable_anthropic_cache_ttl_1h_injection: false,
  rewrite_message_cache_control: false,
  enable_client_dateline_normalization: true,
  antigravity_user_agent_version: "",
  openai_codex_user_agent: "",
  payment_enabled: true,
  creative_enabled: true,
  creative_model_settings: [],
  payment_min_amount: 1,
  payment_max_amount: 10000,
  payment_daily_limit: 50000,
  payment_order_timeout_minutes: 30,
  payment_max_pending_orders: 3,
  payment_enabled_types: [],
  payment_balance_disabled: false,
  payment_balance_recharge_multiplier: 1,
  payment_subscription_usd_to_cny_rate: 0,
  payment_recharge_fee_rate: 0,
  payment_method_fees: {},
  payment_load_balance_strategy: "round-robin",
  payment_product_name_prefix: "",
  payment_product_name_suffix: "",
  payment_help_image_url: "",
  payment_help_text: "",
  payment_cancel_rate_limit_enabled: false,
  payment_cancel_rate_limit_max: 10,
  payment_cancel_rate_limit_window: 1,
  payment_cancel_rate_limit_unit: "day",
  payment_cancel_rate_limit_window_mode: "rolling",
  payment_visible_method_alipay_source: "alipay_direct",
  payment_visible_method_wxpay_source: "invalid-source",
  payment_visible_method_alipay_enabled: true,
  payment_visible_method_wxpay_enabled: true,
  advanced_scheduler_sticky_weighted_enabled: false,
  advanced_scheduler_subscription_priority_enabled: false,
  advanced_scheduler_ewma_error_rate_alpha: "",
  advanced_scheduler_ewma_ttft_alpha: "",
  advanced_scheduler_sticky_escape_enabled: true,
  advanced_scheduler_sticky_escape_ttft_ms: "",
  advanced_scheduler_sticky_escape_error_rate: "",
  advanced_scheduler_lb_top_k: "",
  advanced_scheduler_weight_priority: "",
  advanced_scheduler_weight_load: "",
  advanced_scheduler_weight_queue: "",
  advanced_scheduler_weight_error_rate: "",
  advanced_scheduler_weight_ttft: "",
  advanced_scheduler_weight_reset: "",
  advanced_scheduler_weight_quota_headroom: "",
  advanced_scheduler_weight_previous_response: "",
  advanced_scheduler_weight_session_sticky: "",
  advanced_scheduler_effective_lb_top_k: "7",
  advanced_scheduler_effective_weight_priority: "1",
  advanced_scheduler_effective_weight_load: "1",
  advanced_scheduler_effective_weight_queue: "0.7",
  advanced_scheduler_effective_weight_error_rate: "0.8",
  advanced_scheduler_effective_weight_ttft: "0.5",
  advanced_scheduler_effective_weight_reset: "0",
  advanced_scheduler_effective_weight_quota_headroom: "0",
  advanced_scheduler_effective_weight_previous_response: "5",
  advanced_scheduler_effective_weight_session_sticky: "3",
  advanced_scheduler_effective_ewma_error_rate_alpha: "0.2",
  advanced_scheduler_effective_ewma_ttft_alpha: "0.2",
  advanced_scheduler_effective_sticky_escape_enabled: true,
  advanced_scheduler_effective_sticky_escape_ttft_ms: "15000",
  advanced_scheduler_effective_sticky_escape_error_rate: "0.5",
  openai_provider_quota_auto_pause: {
    default_threshold_5h: 0,
    default_threshold_7d: 0,
  },
  balance_low_notify_enabled: false,
  balance_low_notify_threshold: 0,
  balance_low_notify_recharge_url: "",
  subscription_expiry_notify_enabled: true,
  provider_quota_notify_enabled: false,
  provider_quota_notify_emails: [],
};

function mountView() {
  return mount(SettingsView, {
    global: {
      stubs: {
        "transition-group": true,
        AppLayout: AppLayoutStub,
        Select: SelectStub,
        Toggle: ToggleStub,
        Icon: true,
        ProviderIcon: true,
        ConfirmDialog: true,
        PaymentProviderList: true,
        PaymentProviderDialog: true,
        GroupBadge: true,
        GroupOptionItem: true,
        ProxySelector: true,
        ModelWhitelistSelector: true,
        ImageUpload: ImageUploadStub,
        BackupSettings: true,
      },
    },
  });
}

async function openPaymentTab(wrapper: ReturnType<typeof mountView>) {
  const paymentTabButton = wrapper
    .findAll("button")
    .find((node) => node.text().includes("admin.settings.tabs.payment"));

  expect(paymentTabButton).toBeDefined();
  await paymentTabButton?.trigger("click");
  await flushPromises();
}

async function openSecurityTab(wrapper: ReturnType<typeof mountView>) {
  const securityTabButton = wrapper
    .findAll("button")
    .find((node) => node.text().includes("admin.settings.tabs.security"));

  expect(securityTabButton).toBeDefined();
  await securityTabButton?.trigger("click");
  await flushPromises();
}

// 通过吸底保存条保存：先确认保存条已出现，再提交表单。
async function saveFromSaveBar(wrapper: ReturnType<typeof mountView>) {
  expect(wrapper.find('[data-testid="settings-save-bar"]').exists()).toBe(true);
  await wrapper.find("form").trigger("submit.prevent");
  await flushPromises();
}

async function openGatewayTab(wrapper: ReturnType<typeof mountView>) {
  const gatewayTabButton = wrapper
    .findAll("button")
    .find((node) => node.text().includes("admin.settings.tabs.gateway"));

  expect(gatewayTabButton).toBeDefined();
  await gatewayTabButton?.trigger("click");
  await flushPromises();
}

async function openGatewaySection(
  wrapper: ReturnType<typeof mountView>,
  section:
    | "general"
    | "anthropic"
    | "openai"
    | "grok"
    | "antigravity"
    | "ollamaCloud",
) {
  await wrapper
    .get(`[data-testid="gateway-section-tab-${section}"]`)
    .trigger("click");
  await flushPromises();
}

async function openUsersTab(wrapper: ReturnType<typeof mountView>) {
  const usersTabButton = wrapper
    .findAll("button")
    .find((node) => node.text().includes("admin.settings.tabs.users"));

  expect(usersTabButton).toBeDefined();
  await usersTabButton?.trigger("click");
  await flushPromises();
}

describe("admin SettingsView payment visible method controls", () => {
  beforeEach(() => {
    getSettings.mockReset();
    getCreativeModelCandidates.mockReset();
    updateSettings.mockReset();
    getWebSearchEmulationConfig.mockReset();
    updateWebSearchEmulationConfig.mockReset();
    getAdminApiKey.mockReset();
    getOverloadCooldownSettings.mockReset();
    getOpenAI403CooldownSettings.mockReset();
    updateOpenAI403CooldownSettings.mockReset();
    getRateLimit429CooldownSettings.mockReset();
    updateRateLimit429CooldownSettings.mockReset();
    getStreamTimeoutSettings.mockReset();
    getRectifierSettings.mockReset();
    getBetaPolicySettings.mockReset();
    getOllamaCloudUsageSettings.mockReset();
    updateOllamaCloudUsageSettings.mockReset();
    getGroups.mockReset();
    listProxies.mockReset();
    getProviders.mockReset();
    updateProvider.mockReset();
    createProvider.mockReset();
    deleteProvider.mockReset();
    fetchPublicSettings.mockReset();
    adminSettingsFetch.mockReset();
    showError.mockReset();
    showSuccess.mockReset();
    localeRef.value = "zh-CN";

    getSettings.mockResolvedValue({ ...baseSettingsResponse });
    getCreativeModelCandidates.mockResolvedValue([]);
    updateSettings.mockImplementation(async (payload) => ({
      ...baseSettingsResponse,
      ...payload,
    }));
    getWebSearchEmulationConfig.mockResolvedValue({
      enabled: false,
      providers: [],
    });
    updateWebSearchEmulationConfig.mockResolvedValue({
      enabled: false,
      providers: [],
    });
    getAdminApiKey.mockResolvedValue({
      exists: false,
      masked_key: "",
    });
    getOverloadCooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_minutes: 10,
    });
    getOpenAI403CooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_minutes: 10,
      error_on_threshold_enabled: true,
      threshold_count: 3,
      threshold_window_minutes: 180,
    });
    updateOpenAI403CooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_minutes: 10,
      error_on_threshold_enabled: true,
      threshold_count: 3,
      threshold_window_minutes: 180,
    });
    getRateLimit429CooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_seconds: 5,
    });
    updateRateLimit429CooldownSettings.mockImplementation(async (payload) => payload);
    getStreamTimeoutSettings.mockResolvedValue({
      enabled: true,
      action: "temp_unsched",
      temp_unsched_minutes: 5,
      threshold_count: 3,
      threshold_window_minutes: 10,
    });
    getRectifierSettings.mockResolvedValue({
      enabled: true,
      thinking_signature_enabled: true,
      thinking_budget_enabled: true,
      apikey_signature_enabled: false,
      apikey_signature_patterns: [],
    });
    getBetaPolicySettings.mockResolvedValue({
      rules: [],
    });
    getOllamaCloudUsageSettings.mockResolvedValue({
      enabled: false,
      interval_minutes: 60,
      debounce_minutes: 1,
    });
    updateOllamaCloudUsageSettings.mockImplementation(async (payload) => payload);
    getGroups.mockResolvedValue([]);
    listProxies.mockResolvedValue({
      items: [],
    });
    getProviders.mockResolvedValue({
      data: [],
    });
    fetchPublicSettings.mockResolvedValue(undefined);
    adminSettingsFetch.mockResolvedValue(undefined);
  });

  it("搜索测试默认隐藏，点击测试后以弹窗展示并可关闭", async () => {
    getWebSearchEmulationConfig.mockResolvedValue({ enabled: true, providers: [] });
    testWebSearchEmulation.mockReset();
    testWebSearchEmulation.mockResolvedValue({
      provider: "brave",
      results: [
        { title: "测试结果", url: "https://example.com", snippet: "结果摘要" },
      ],
    });
    const wrapper = mountView();
    const querySelector =
      'input[placeholder="admin.settings.webSearchEmulation.testDefaultQuery"]';
    try {
      await flushPromises();
      await openGatewayTab(wrapper);
      // 挂载 BaseDialog 检查组件注册与 Teleport 位置。
      expect(wrapper.findComponent(BaseDialog).exists()).toBe(true);
      expect(wrapper.find("basedialog").exists()).toBe(false);
      expect(wrapper.find(querySelector).exists()).toBe(false);
      expect(document.body.querySelector(querySelector)).toBeNull();

      await openGatewaySection(wrapper, "anthropic");
      const providerCard = wrapper.get(
        '[data-testid="gateway-card-web-search-emulation"]',
      );
      await providerCard.findAll("button")
        .find(button => button.text() === "admin.settings.webSearchEmulation.addProvider")!
        .trigger("click");
      const openTest = providerCard.findAll("button")
        .find(button => button.text() === "admin.settings.webSearchEmulation.test")!;
      await openTest.trigger("click");
      await flushPromises();

      const dialog = new DOMWrapper(document.body).get('[role="dialog"]');
      expect(dialog.text()).toContain("admin.settings.webSearchEmulation.testResultTitle");
      expect(document.body.classList.contains("modal-open")).toBe(true);
      await dialog.get(querySelector).setValue("测试查询");
      await dialog.findAll("button")
        .find(button => button.text() === "admin.settings.webSearchEmulation.test")!
        .trigger("click");
      await flushPromises();
      expect(testWebSearchEmulation).toHaveBeenCalledWith("测试查询");
      expect(dialog.text()).toContain("测试结果");
      expect(updateSettings).not.toHaveBeenCalled();

      await dialog.findAll("button")
        .find(button => button.text() === "common.close")!
        .trigger("click");
      await flushPromises();
      await vi.waitFor(() => {
        expect(document.body.querySelector(querySelector)).toBeNull();
      });
      expect(document.body.classList.contains("modal-open")).toBe(false);

      await openTest.trigger("click");
      await flushPromises();
      expect(new DOMWrapper(document.body).get('[role="dialog"]').text())
        .not.toContain("测试结果");
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
      await flushPromises();
      await vi.waitFor(() => {
        expect(document.body.querySelector(querySelector)).toBeNull();
      });
    } finally {
      wrapper.unmount();
    }
  });

  it("renders panel rate limit card and saves settings", async () => {
    getPanelRateLimitSettings.mockClear();
    updatePanelRateLimitSettings.mockClear();
    getPanelRateLimitSettings.mockResolvedValue({
      enabled: true,
      user_rpm: 240,
      heavy_rpm: 60,
      exempt_admin: true,
      public_ip_rpm: 300,
    });
    updatePanelRateLimitSettings.mockImplementation(async (payload) => payload);

    const wrapper = mountView();
    await flushPromises();

    expect(getPanelRateLimitSettings).toHaveBeenCalled();
    expect(wrapper.text()).toContain("admin.settings.panelRateLimit.title");
    expect(wrapper.text()).toContain("admin.settings.panelRateLimit.proxySafeNote");

    const userRpmInput = wrapper.find('[data-testid="panel-rate-limit-user-rpm"]');
    expect(userRpmInput.exists()).toBe(true);
    await userRpmInput.setValue("120");

    await saveFromSaveBar(wrapper);

    expect(updatePanelRateLimitSettings).toHaveBeenCalledWith({
      enabled: true,
      user_rpm: 120,
      heavy_rpm: 60,
      exempt_admin: true,
      public_ip_rpm: 300,
    });
    // 只修改了面板限流时，全局设置不重复提交。
    expect(updateSettings).not.toHaveBeenCalled();
    expect(showSuccess).toHaveBeenCalled();
  });

  it("renders and submits usage ranking controls with the selected metric kept visible", async () => {
    getSettings.mockResolvedValue({
      ...baseSettingsResponse,
      usage_ranking_limit: 12,
      usage_ranking_enabled: true,
      usage_ranking_sort_by: "actual_cost",
      usage_ranking_show_total_tokens: false,
      usage_ranking_show_requests: false,
      usage_ranking_show_actual_cost: false,
    });
    getCreativeModelCandidates.mockResolvedValue([]);

    const wrapper = mountView();
    await flushPromises();

    expect(wrapper.text()).toContain("admin.settings.usageRanking.title");
    await wrapper.find("form").trigger("submit");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        usage_ranking_limit: 12,
        usage_ranking_enabled: true,
        usage_ranking_sort_by: "actual_cost",
        usage_ranking_show_total_tokens: false,
        usage_ranking_show_requests: false,
        usage_ranking_show_actual_cost: true,
      }),
    );
  });

  it("loads and saves Google One Tap settings with the current browser origin", async () => {
    getSettings.mockResolvedValue({
      ...baseSettingsResponse,
      google_oauth_enabled: true,
      google_one_tap_enabled: true,
      google_oauth_client_id: "google-web-client",
      google_oauth_client_secret_configured: true,
      google_oauth_redirect_url:
        "https://app.example/api/v1/auth/oauth/google/callback",
      google_oauth_frontend_redirect_url: "/auth/oauth/callback",
    });

    const wrapper = mountView();
    await flushPromises();

    expect(wrapper.text()).toContain("Google One Tap");
    expect(wrapper.text()).toContain(window.location.origin);

    await wrapper.get("form").trigger("submit.prevent");
    await flushPromises();
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        google_oauth_enabled: true,
        google_one_tap_enabled: true,
        google_oauth_client_id: "google-web-client",
      }),
    );
  });

  it("does not render legacy visible payment method controls", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openPaymentTab(wrapper);

    expect(wrapper.text()).not.toContain("可见方式");
    expect(wrapper.text()).not.toContain("支付来源");
  });

  it("人机验证切换到腾讯天御并保存四项配置", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);

    const masterToggle = wrapper.get('[data-testid="captcha-enabled-toggle"]');
    await masterToggle.setValue(true);
    // 默认选中 Turnstile
    expect(wrapper.text()).toContain("admin.settings.turnstile.siteKey");

    await wrapper.get('[data-testid="captcha-provider-tencent"]').trigger("click");
    await flushPromises();

    const card = wrapper
      .findAll(".card")
      .find((node) => node.text().includes("admin.settings.captcha.title"));
    expect(card).toBeDefined();
    expect(card!.text()).not.toContain("admin.settings.turnstile.siteKey");
    expect(card!.get('a[href="https://console.cloud.tencent.com/captcha"]').exists()).toBe(true);
    expect(card!.get('a[href="https://console.cloud.tencent.com/cam/capi"]').exists()).toBe(true);
    expect(
      card!.get('a[href="https://cloud.tencent.com/document/product/1110/36841"]').exists(),
    ).toBe(true);
    const inputs = card!.findAll("input").filter((input) => input.attributes("type") !== "checkbox");
    await inputs[0]!.setValue("123456789");
    await inputs[1]!.setValue("app-secret-value");
    await inputs[2]!.setValue("cloud-secret-id-value");
    await inputs[3]!.setValue("cloud-secret-key-value");

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        turnstile_enabled: false,
        tencent_captcha_enabled: true,
        aliyun_captcha_enabled: false,
        tencent_captcha_app_id: "123456789",
        tencent_captcha_app_secret_key: "app-secret-value",
        tencent_captcha_cloud_secret_id: "cloud-secret-id-value",
        tencent_captcha_cloud_secret_key: "cloud-secret-key-value",
        tencent_captcha_region: "cn",
      }),
    );
  });

  it("腾讯天御切换到国际站后保存站点并更新控制台入口", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);

    await wrapper.get('[data-testid="captcha-enabled-toggle"]').setValue(true);
    await wrapper.get('[data-testid="captcha-provider-tencent"]').trigger("click");
    await wrapper.get('[data-testid="tencent-captcha-region-intl"]').trigger("click");

    const card = wrapper
      .findAll(".card")
      .find((node) => node.text().includes("admin.settings.captcha.title"));
    expect(card).toBeDefined();
    expect(card!.get('a[href="https://console.tencentcloud.com/captcha/graphical"]').exists()).toBe(
      true,
    );
    expect(card!.get('a[href="https://console.tencentcloud.com/cam/capi"]').exists()).toBe(true);

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        tencent_captcha_enabled: true,
        tencent_captcha_region: "intl",
      }),
    );
  });

  it("人机验证切换到阿里云并保存配置", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);

    const masterToggle = wrapper.get('[data-testid="captcha-enabled-toggle"]');
    await masterToggle.setValue(true);

    await wrapper.get('[data-testid="captcha-provider-aliyun"]').trigger("click");
    await flushPromises();

    const card = wrapper
      .findAll(".card")
      .find((node) => node.text().includes("admin.settings.captcha.title"));
    expect(card).toBeDefined();
    expect(card!.text()).toContain("admin.settings.aliyunCaptcha.region");
    expect(card!.text()).not.toContain("admin.settings.turnstile.siteKey");
    const inputs = card!.findAll("input").filter((input) => input.attributes("type") !== "checkbox");
    await inputs[0]!.setValue("prefix-1");
    await inputs[1]!.setValue("scene-1");
    await inputs[2]!.setValue("ak-id");
    await inputs[3]!.setValue("ak-secret-value");

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        turnstile_enabled: false,
        tencent_captcha_enabled: false,
        aliyun_captcha_enabled: true,
        aliyun_captcha_prefix: "prefix-1",
        aliyun_captcha_scene_id: "scene-1",
        aliyun_captcha_access_key_id: "ak-id",
        aliyun_captcha_access_key_secret: "ak-secret-value",
        aliyun_captcha_region: "cn",
      }),
    );
  });

  it("关闭人机验证总开关会同时关闭所有服务商", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      tencent_captcha_enabled: true,
      tencent_captcha_app_id: "123456789",
      tencent_captcha_app_secret_key_configured: true,
      tencent_captcha_cloud_secret_id_configured: true,
      tencent_captcha_cloud_secret_key_configured: true,
    });
    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);

    const masterToggle = wrapper.get('[data-testid="captcha-enabled-toggle"]');
    expect((masterToggle.element as HTMLInputElement).checked).toBe(true);
    // 加载后选中项跟随已启用的服务商
    expect(wrapper.text()).toContain("admin.settings.tencentCaptcha.appId");

    await masterToggle.setValue(false);
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        turnstile_enabled: false,
        tencent_captcha_enabled: false,
        aliyun_captcha_enabled: false,
      }),
    );
  });
  it("loads, edits, validates, and saves forwarded client-IP headers", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      api_key_acl_trust_forwarded_ip: false,
      forwarded_client_ip_headers: ["cf-connecting-ip", "X-Real-IP"],
    });
    const wrapper = mountView();

    await flushPromises();
    await openSecurityTab(wrapper);

    const card = wrapper
      .findAll(".card")
      .find((node) => node.text().includes("admin.settings.apiKeyAcl.title"));
    expect(card).toBeDefined();
    const toggle = card!.get('input[type="checkbox"]');
    expect((toggle.element as HTMLInputElement).checked).toBe(false);
    expect(card!.find('[data-testid="forwarded-client-ip-headers-input"]').exists()).toBe(false);

    await toggle.setValue(true);
    expect(card!.findAll('[data-testid="forwarded-client-ip-header-tag"]')).toHaveLength(2);
    expect(card!.text()).toContain("Cf-Connecting-Ip");
    expect(card!.text()).toContain("X-Real-Ip");
    showError.mockClear();

    const input = card!.get('[data-testid="forwarded-client-ip-headers-input"]');
    await input.setValue("x-client-ip");
    await input.trigger("keydown", { key: "Enter" });
    await input.setValue("X-CLIENT-IP");
    await input.trigger("keydown", { key: "Enter" });
    await input.setValue("invalid header");
    await input.trigger("keydown", { key: "Enter" });
    expect(showError).toHaveBeenCalledTimes(1);
    expect(card!.findAll('[data-testid="forwarded-client-ip-header-tag"]')).toHaveLength(3);

    const realIpTag = card!
      .findAll('[data-testid="forwarded-client-ip-header-tag"]')
      .find((tag) => tag.text().includes("X-Real-Ip"));
    expect(realIpTag).toBeDefined();
    await realIpTag!.get("button").trigger("click");
    expect(card!.text()).not.toContain("X-Real-Ip");

    await toggle.setValue(false);
    expect(card!.find('[data-testid="forwarded-client-ip-headers-input"]').exists()).toBe(false);
    await toggle.setValue(true);
    expect(card!.text()).toContain("X-Client-Ip");

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        api_key_acl_trust_forwarded_ip: true,
        forwarded_client_ip_headers: ["Cf-Connecting-Ip", "X-Client-Ip"],
      }),
    );
  });

  it("links payment guidance to the maintained TokenRouter guide", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openPaymentTab(wrapper);

    const paymentLinks = wrapper
      .findAll("a")
      .filter((node) => node.text() === "查看支付配置说明");

    expect(paymentLinks).toHaveLength(1);
    expect(paymentLinks[0]?.attributes("href")).toBe(
      "https://github.com/TokenFlux/TokenRouter/blob/main/docs/guides/payments/configuration.md",
    );
  });

  it("does not submit legacy visible payment method settings", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openPaymentTab(wrapper);
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    const payload = updateSettings.mock.calls[0]?.[0];
    expect(payload).not.toHaveProperty("payment_visible_method_alipay_source");
    expect(payload).not.toHaveProperty("payment_visible_method_wxpay_source");
    expect(payload).not.toHaveProperty("payment_visible_method_alipay_enabled");
    expect(payload).not.toHaveProperty("payment_visible_method_wxpay_enabled");
  });

  it("submits per-method payment fee settings", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openPaymentTab(wrapper);

    const stripeCheckbox = wrapper.findAll('input[type="checkbox"]').find((node) => {
      const label = node.element.closest('label')
      return label?.textContent?.includes('Stripe')
    })
    expect(stripeCheckbox).toBeDefined()
    await stripeCheckbox?.setValue(true)

    const stripeRow = stripeCheckbox?.element.closest('.grid')
    const fixedFeeInput = wrapper.findAll('input[type="number"]').find((node) => {
      return stripeRow?.contains(node.element) && node.attributes('max') !== '100'
    })
    expect(fixedFeeInput).toBeDefined()
    await fixedFeeInput?.setValue('2.5')

    const feeRateInput = wrapper.findAll('input[type="number"]').find((node) => {
      return stripeRow?.contains(node.element) && node.attributes('max') === '100'
    })
    expect(feeRateInput).toBeDefined()
    await feeRateInput?.setValue('2.2')

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        payment_method_fees: expect.objectContaining({
          stripe: {
            enabled: true,
            fixed_fee: 2.5,
            fee_rate: 2.2,
          },
        }),
      }),
    );
  });

  it("submits the admin recharge affiliate rebate setting", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      affiliate_enabled: true,
      affiliate_admin_recharge_enabled: true,
    });

    const wrapper = mountView();

    await flushPromises();
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        affiliate_admin_recharge_enabled: true,
      }),
    );
  });

  it("submits Anthropic cache TTL injection gateway setting", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      enable_anthropic_cache_ttl_1h_injection: true,
    });

    const wrapper = mountView();

    await flushPromises();
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        enable_anthropic_cache_ttl_1h_injection: true,
      }),
    );
  });

  it("submits message cache_control rewrite gateway setting", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      rewrite_message_cache_control: true,
    });

    const wrapper = mountView();

    await flushPromises();
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        rewrite_message_cache_control: true,
      }),
    );
  });

  it("preserves OpenAI Fast policy user IDs when saving", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      openai_fast_policy_settings: {
        rules: [
          {
            service_tier: "priority",
            action: "pass",
            scope: "all",
            user_ids: [42, 43],
            model_whitelist: [],
            fallback_action: "pass",
          },
        ],
      },
    });

    const wrapper = mountView();

    await flushPromises();
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        openai_fast_policy_settings: {
          rules: [
            expect.objectContaining({
              user_ids: [42, 43],
            }),
          ],
        },
      }),
    );
  });

  it("submits Claude OAuth system prompt gateway settings", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      enable_claude_oauth_system_prompt_injection: false,
      claude_oauth_system_prompt: " Custom prompt ",
      claude_oauth_system_prompt_blocks:
        ' {"blocks":[{"type":"text","text":"{billing_header}"}]} ',
    });

    const wrapper = mountView();

    await flushPromises();
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        enable_claude_oauth_system_prompt_injection: false,
        claude_oauth_system_prompt: "Custom prompt",
        claude_oauth_system_prompt_blocks:
          '{"blocks":[{"type":"text","text":"{billing_header}"}]}',
      }),
    );
  });

  it("renders and submits OpenAI quota auto-pause gateway settings", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      openai_provider_quota_auto_pause: {
        default_threshold_5h: 0.95,
        default_threshold_7d: 0.9,
      },
    });

    const wrapper = mountView();

    await flushPromises();

    expect(wrapper.text()).toContain("OpenAI 提供商配额自动暂停");
    const fiveHourInput = wrapper.get('[data-testid="settings-openai-quota-auto-pause-5h"]');
    const sevenDayInput = wrapper.get('[data-testid="settings-openai-quota-auto-pause-7d"]');
    expect((fiveHourInput.element as HTMLInputElement).value).toBe("95");
    expect((sevenDayInput.element as HTMLInputElement).value).toBe("90");

    await fiveHourInput.setValue("88.5");
    await sevenDayInput.setValue("77.5");
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        openai_provider_quota_auto_pause: {
          default_threshold_5h: 0.885,
          default_threshold_7d: 0.775,
        },
      }),
    );
  });

  it("submits Antigravity user agent version gateway setting", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      antigravity_user_agent_version: "1.23.2",
    });

    const wrapper = mountView();

    await flushPromises();
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        antigravity_user_agent_version: "1.23.2",
      }),
    );
  });

  it("updates provider enablement immediately and reloads providers", async () => {
    const provider = {
      id: 7,
      provider_key: "alipay",
      name: "Official Alipay",
      config: {},
      supported_types: ["alipay"],
      enabled: false,
      payment_mode: "",
      refund_enabled: false,
      allow_user_refund: false,
      limits: "",
      sort_order: 0,
    };
    getProviders.mockReset();
    getProviders
      .mockResolvedValueOnce({ data: [provider] })
      .mockResolvedValueOnce({ data: [{ ...provider, enabled: true }] });
    updateProvider.mockResolvedValue({ data: { ...provider, enabled: true } });

    const PaymentProviderListStub = defineComponent({
      emits: ["toggleField"],
      setup(_, { emit }) {
        return () =>
          h(
            "button",
            {
              class: "provider-toggle-stub",
              onClick: () => emit("toggleField", provider, "enabled"),
            },
            "toggle provider",
          );
      },
    });

    const wrapper = mount(SettingsView, {
      global: {
        stubs: {
        "transition-group": true,
          AppLayout: AppLayoutStub,
          Select: SelectStub,
          Toggle: ToggleStub,
          Icon: true,
          ConfirmDialog: true,
          PaymentProviderList: PaymentProviderListStub,
          PaymentProviderDialog: true,
          GroupBadge: true,
          GroupOptionItem: true,
          ProxySelector: true,
          ModelWhitelistSelector: true,
          ImageUpload: ImageUploadStub,
          BackupSettings: true,
        },
      },
    });

    await flushPromises();
    await openPaymentTab(wrapper);
    await wrapper.get(".provider-toggle-stub").trigger("click");
    await flushPromises();

    expect(updateProvider).toHaveBeenCalledWith(7, { enabled: true });
    expect(getProviders).toHaveBeenCalledTimes(2);
  });

  it("summarizes target and other-model actions, then switches to all models", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      openai_fast_policy_settings: {
        rules: [
          {
            service_tier: "all",
            action: "filter",
            scope: "all",
            model_whitelist: ["gpt-5.6-sol"],
            fallback_action: "pass",
          },
        ],
      },
    });
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);

    const summary = wrapper.get('[data-testid="openai-fast-policy-summary-0"]');
    expect(summary.text()).toContain("目标模型");
    expect(summary.text()).toContain("过滤");
    expect(summary.text()).toContain("其他模型");
    expect(summary.text()).toContain("透传");

    await wrapper
      .get(
        '[role="group"][aria-labelledby="openai-fast-policy-models-label-0"] input[type="text"]',
      )
      .setValue("");
    expect(summary.text()).toContain("全部模型");
    expect(summary.text()).toContain("过滤");
    expect(summary.text()).not.toContain("其他模型");
    expect(summary.text()).not.toContain("透传");
  });

  it("normalizes null provider supported types from API", async () => {
    const provider = {
      id: 42,
      provider_key: "easypay",
      name: "EasyPay",
      config: {},
      supported_types: null as unknown as string[],
      enabled: true,
      payment_mode: "",
      refund_enabled: false,
      allow_user_refund: false,
      limits: "",
      sort_order: 0,
    };
    getProviders.mockReset();
    getProviders.mockResolvedValue({ data: [provider] });

    let receivedProviders: Array<Record<string, unknown>> = [];
    const PaymentProviderListCapture = defineComponent({
      props: {
        providers: {
          type: Array,
          default: () => [],
        },
      },
      setup(props) {
        receivedProviders = props.providers as Array<Record<string, unknown>>;
        return () => h("div", { class: "provider-list-capture" });
      },
    });

    const wrapper = mount(SettingsView, {
      global: {
        stubs: {
        "transition-group": true,
          AppLayout: AppLayoutStub,
          Select: SelectStub,
          Toggle: ToggleStub,
          Icon: true,
          ConfirmDialog: true,
          PaymentProviderList: PaymentProviderListCapture,
          PaymentProviderDialog: true,
          GroupBadge: true,
          GroupOptionItem: true,
          ProxySelector: true,
          ModelWhitelistSelector: true,
          ImageUpload: ImageUploadStub,
          BackupSettings: true,
        },
      },
    });

    await flushPromises();
    await openPaymentTab(wrapper);

    expect(receivedProviders).toHaveLength(1);
    expect(Array.isArray(receivedProviders[0].supported_types)).toBe(true);
    expect(receivedProviders[0].supported_types).toEqual([]);
  });

  it("ignores repeated provider type clicks while the same provider is updating", async () => {
    const provider = {
      id: 8,
      provider_key: "easypay",
      name: "EasyPay",
      config: {},
      supported_types: ["alipay"],
      enabled: true,
      payment_mode: "",
      refund_enabled: false,
      allow_user_refund: false,
      limits: "",
      sort_order: 0,
    };
    let resolveUpdate: ((value: unknown) => void) | undefined;
    getProviders.mockReset();
    getProviders.mockResolvedValue({ data: [provider] });
    updateProvider.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveUpdate = resolve;
        }),
    );

    const PaymentProviderListStub = defineComponent({
      emits: ["toggleType"],
      setup(_, { emit }) {
        return () =>
          h(
            "button",
            {
              class: "provider-type-stub",
              onClick: () => emit("toggleType", provider, "wxpay"),
            },
            "toggle type",
          );
      },
    });

    const wrapper = mount(SettingsView, {
      global: {
        stubs: {
        "transition-group": true,
          AppLayout: AppLayoutStub,
          Select: SelectStub,
          Toggle: ToggleStub,
          Icon: true,
          ConfirmDialog: true,
          PaymentProviderList: PaymentProviderListStub,
          PaymentProviderDialog: true,
          GroupBadge: true,
          GroupOptionItem: true,
          ProxySelector: true,
          ModelWhitelistSelector: true,
          ImageUpload: ImageUploadStub,
          BackupSettings: true,
        },
      },
    });

    await flushPromises();
    await openPaymentTab(wrapper);
    await wrapper.get(".provider-type-stub").trigger("click");
    await wrapper.get(".provider-type-stub").trigger("click");

    expect(updateProvider).toHaveBeenCalledTimes(1);
    expect(updateProvider).toHaveBeenCalledWith(8, {
      supported_types: ["alipay", "wxpay"],
    });

    resolveUpdate?.({ data: { ...provider, supported_types: ["alipay", "wxpay"] } });
    await flushPromises();
  });

  it("ignores Stripe subtype toggle events because Checkout uses dashboard payment methods", async () => {
    const provider = {
      id: 9,
      provider_key: "stripe",
      name: "Stripe",
      config: {},
      supported_types: ["card", "wxpay"],
      enabled: true,
      payment_mode: "",
      refund_enabled: false,
      allow_user_refund: false,
      limits: "",
      sort_order: 0,
    };
    getProviders.mockReset();
    getProviders.mockResolvedValue({ data: [provider] });

    const PaymentProviderListStub = defineComponent({
      emits: ["toggleType"],
      setup(_, { emit }) {
        return () =>
          h(
            "button",
            {
              class: "provider-type-stub",
              onClick: () => emit("toggleType", provider, "link"),
            },
            "toggle type",
          );
      },
    });

    const wrapper = mount(SettingsView, {
      global: {
        stubs: {
        "transition-group": true,
          AppLayout: AppLayoutStub,
          Select: SelectStub,
          Toggle: ToggleStub,
          Icon: true,
          ConfirmDialog: true,
          PaymentProviderList: PaymentProviderListStub,
          PaymentProviderDialog: true,
          GroupBadge: true,
          GroupOptionItem: true,
          ProxySelector: true,
          ModelWhitelistSelector: true,
          ImageUpload: ImageUploadStub,
          BackupSettings: true,
        },
      },
    });

    await flushPromises();
    await openPaymentTab(wrapper);
    await wrapper.get(".provider-type-stub").trigger("click");

    expect(updateProvider).not.toHaveBeenCalled();
  });

  it("groups gateway settings into six platform sections with General selected by default", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);

    const sectionTabs = wrapper.findAll('[data-testid^="gateway-section-tab-"]');
    expect(sectionTabs).toHaveLength(6);
    expect(sectionTabs.map((tab) => tab.text())).toEqual([
      "通用",
      "Anthropic",
      "OpenAI",
      "Grok",
      "Antigravity",
      "Ollama Cloud",
    ]);
    expect(
      wrapper
        .get('[data-testid="gateway-section-tab-general"]')
        .attributes("aria-selected"),
    ).toBe("true");
    expect(
      wrapper.get('[data-testid="gateway-card-overload-cooldown"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.get('[data-testid="gateway-card-request-rectifier"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.get('[data-testid="gateway-card-user-prompt-replacement"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.get('[data-testid="gateway-card-openai-403-cooldown"]').isVisible(),
    ).toBe(false);
    expect(
      wrapper.get('[data-testid="ollama-cloud-usage-global-settings"]').isVisible(),
    ).toBe(false);

    const providerBrands = [
      ["anthropic", "Anthropic"],
      ["openai", "OpenAI"],
      ["grok", "Grok"],
      ["antigravity", "Google"],
      ["ollamaCloud", "Ollama"],
    ];
    for (const [section, brand] of providerBrands) {
      const icon = wrapper
        .get(`[data-testid="gateway-section-tab-${section}"]`)
        .get("provider-icon-stub");
      expect(icon.attributes("brand")).toBe(brand);
      expect(icon.attributes("color")).toBe("currentColor");
    }
  });

  it("shows the representative cards for each gateway platform section", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);

    await openGatewaySection(wrapper, "anthropic");
    expect(
      wrapper
        .get('[data-testid="gateway-card-request-rectifier"]')
        .attributes("style"),
    ).toContain("display: none");
    expect(
      wrapper.get('[data-testid="gateway-card-claude-code"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.get('[data-testid="gateway-forwarding-anthropic"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.get('[data-testid="gateway-card-web-search-emulation"]').isVisible(),
    ).toBe(true);

    await openGatewaySection(wrapper, "openai");
    expect(
      wrapper.get('[data-testid="gateway-card-openai-403-cooldown"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.find('[data-testid="gateway-card-openai-oauth-defaults"]').exists(),
    ).toBe(false);
    expect(
      wrapper.get('[data-testid="gateway-card-openai-scheduling"]').isVisible(),
    ).toBe(true);
    expect(wrapper.get('[data-testid="gateway-card-scheduling"]').isVisible()).toBe(false);
    expect(
      wrapper
        .get('[data-testid="gateway-card-user-prompt-replacement"]')
        .attributes("style"),
    ).toContain("display: none");

    await openGatewaySection(wrapper, "grok");
    expect(
      wrapper.get('[data-testid="gateway-forwarding-grok"]').isVisible(),
    ).toBe(true);
    expect(wrapper.get('[data-testid="grok-default-text-model"]').isVisible()).toBe(
      true,
    );
    expect(
      wrapper.get('[data-testid="grok-default-base-url-mode"]').isVisible(),
    ).toBe(true);

    await openGatewaySection(wrapper, "antigravity");
    expect(
      wrapper.get('[data-testid="gateway-forwarding-antigravity"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.get('[data-testid="gateway-forwarding-openai"]').isVisible(),
    ).toBe(false);
    expect(
      wrapper
        .get('[data-testid="gateway-card-request-rectifier"]')
        .attributes("style"),
    ).toContain("display: none");
    expect(
      wrapper
        .get('[data-testid="gateway-card-user-prompt-replacement"]')
        .attributes("style"),
    ).toContain("display: none");

    await openGatewaySection(wrapper, "ollamaCloud");
    expect(
      wrapper.get('[data-testid="ollama-cloud-usage-global-settings"]').isVisible(),
    ).toBe(true);
    expect(
      wrapper.get('[data-testid="gateway-card-overload-cooldown"]').isVisible(),
    ).toBe(false);
  });

  it("supports keyboard navigation between gateway platform sections", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);

    await wrapper
      .get('[data-testid="gateway-section-tab-general"]')
      .trigger("keydown", { key: "ArrowRight" });
    expect(
      wrapper
        .get('[data-testid="gateway-section-tab-anthropic"]')
        .attributes("aria-selected"),
    ).toBe("true");

    await wrapper
      .get('[data-testid="gateway-section-tab-anthropic"]')
      .trigger("keydown", { key: "End" });
    expect(
      wrapper
        .get('[data-testid="gateway-section-tab-ollamaCloud"]')
        .attributes("aria-selected"),
    ).toBe("true");

    await wrapper
      .get('[data-testid="gateway-section-tab-ollamaCloud"]')
      .trigger("keydown", { key: "Home" });
    expect(
      wrapper
        .get('[data-testid="gateway-section-tab-general"]')
        .attributes("aria-selected"),
    ).toBe("true");
  });

  it("requires explicit groups and omits platform quota settings", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openGatewayTab(wrapper);
    expect(wrapper.find('[data-testid="gateway-allow-ungrouped-key"]').exists()).toBe(false);
  });

  it("omits the retired CCH signing setting from the UI and update payload", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      enable_cch_signing: true,
    });
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);
    await openGatewaySection(wrapper, "anthropic");

    expect(wrapper.text()).not.toContain(
      "admin.settings.gatewayForwarding.cchSigning",
    );

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings.mock.calls[0]?.[0]).not.toHaveProperty(
      "enable_cch_signing",
    );
  });

  it("renders the advanced scheduler only in General and omits the retired global switch", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);

    const advancedCard = wrapper.get(
      '[data-testid="gateway-scheduling-general-advanced"]',
    );
    const generalCard = wrapper.get(
      '[data-testid="gateway-scheduling-general"]',
    );
    expect(generalCard.isVisible()).toBe(true);
    expect(advancedCard.isVisible()).toBe(true);
    // settings-section 类让它和前一个分区之间显示分隔线。
    expect(advancedCard.classes()).toContain("settings-section");
    expect(advancedCard.find("h4").text()).toBe("通用高级调度器");
    expect(
      advancedCard
        .get('[data-testid="advanced-scheduler-help"] button')
        .attributes("title"),
    ).toBe("查看高级调度器评分与选择原理");
    expect(advancedCard.text()).toContain("通用高级调度器");
    expect(wrapper.text()).not.toContain("OpenAI 实验调度策略");
    expect(advancedCard.text()).toContain("EWMA");
    expect(generalCard.text()).toContain("强制粘性切换");
    expect(advancedCard.text()).not.toContain("强制粘性切换");

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        advanced_scheduler_sticky_weighted_enabled: false,
        advanced_scheduler_subscription_priority_enabled: false,
        advanced_scheduler_ewma_error_rate_alpha: "",
        advanced_scheduler_ewma_ttft_alpha: "",
        advanced_scheduler_sticky_escape_enabled: true,
        advanced_scheduler_sticky_escape_ttft_ms: "",
        advanced_scheduler_sticky_escape_error_rate: "",
      }),
    );
    expect(updateSettings.mock.calls[0]?.[0]).not.toHaveProperty(
      "advanced_scheduler_enabled",
    );

    await openGatewaySection(wrapper, "openai");
    expect(
      wrapper.get('[data-testid="settings-openai-quota-auto-pause-5h"]').isVisible(),
    ).toBe(true);
  });

  it("does not render removed upstream billing probe and rate scheduling controls", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);

    expect(wrapper.find('[data-testid="upstream-billing-probe-settings"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="openai-low-rate-priority-toggle"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="openai-oauth-scheduling-rate-multiplier"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("上游倍率自动探测");
    expect(wrapper.text()).not.toContain("OAuth 调度参考倍率");
  });

  it("loads fail-safe-off Ollama Cloud usage refresh settings and saves an explicit opt-in", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openGatewayTab(wrapper);
    await openGatewaySection(wrapper, "ollamaCloud");

    const card = wrapper.get('[data-testid="ollama-cloud-usage-global-settings"]');
    expect(card.isVisible()).toBe(true);
    expect(
      (card.get('[data-testid="ollama-cloud-usage-global-enabled"]').element as HTMLInputElement)
        .checked,
    ).toBe(false);
    expect(card.find('[data-testid="ollama-cloud-usage-global-interval"]').exists()).toBe(false);

    await card.get('[data-testid="ollama-cloud-usage-global-enabled"]').setValue(true);
    await card.get('[data-testid="ollama-cloud-usage-global-debounce"]').setValue(3);
    await card.get('[data-testid="ollama-cloud-usage-global-interval"]').setValue(90);
    await saveFromSaveBar(wrapper);

    expect(updateOllamaCloudUsageSettings).toHaveBeenCalledWith({
      enabled: true,
      interval_minutes: 90,
      debounce_minutes: 3,
    });
  });

  it("passes translated upload and remove labels to the payment help image uploader", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openPaymentTab(wrapper);

    const imageUploads = wrapper.findAll(".image-upload-stub");
    expect(imageUploads.length).toBeGreaterThan(0);

    const paymentHelpImageUpload = imageUploads.find(
      (node) => node.attributes("data-placeholder") === "admin.settings.payment.helpImagePlaceholder",
    );

    expect(paymentHelpImageUpload).toBeDefined();
    expect(paymentHelpImageUpload?.attributes("data-upload-label")).toBe("上传图片");
    expect(paymentHelpImageUpload?.attributes("data-remove-label")).toBe("移除");
  });

  it("loads creative model candidates and submits the configured capability subset", async () => {
    const creativeSettings = [{
      group_id: 12,
      model: "gpt-image-2",
      operations: ["generate", "inpaint"],
    }];
    getSettings.mockResolvedValue({
      ...baseSettingsResponse,
      creative_model_settings: creativeSettings,
    });
    getCreativeModelCandidates.mockResolvedValue([{
      group_id: 12,
      group_name: "Images",
      platform: "openai",
      model: "gpt-image-2",
      operations: ["generate", "edit", "inpaint"],
    }]);

    const wrapper = mountView();
    await flushPromises();
    expect(getCreativeModelCandidates).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("admin.settings.features.creative.modelSettings.title");

    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({ creative_model_settings: creativeSettings }),
    );
  });

  it("removes stale Gemini inpaint when saving creative model settings", async () => {
    getSettings.mockResolvedValue({
      ...baseSettingsResponse,
      creative_model_settings: [{
        group_id: 12,
        model: "gemini-3.1-flash-image",
        operations: ["generate", "inpaint"],
      }],
    });
    getCreativeModelCandidates.mockResolvedValue([{
      group_id: 12,
      group_name: "Gemini Images",
      platform: "gemini",
      model: "gemini-3.1-flash-image",
      operations: ["generate", "edit"],
    }]);

    const wrapper = mountView();
    await flushPromises();
    await wrapper.find("form").trigger("submit");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        creative_model_settings: [{
          group_id: 12,
          model: "gemini-3.1-flash-image",
          operations: ["generate"],
        }],
      }),
    );
  });

  it("drops a Gemini setting that only contains stale inpaint", async () => {
    getSettings.mockResolvedValue({
      ...baseSettingsResponse,
      creative_model_settings: [{
        group_id: 12,
        model: "gemini-3.1-flash-image",
        operations: ["inpaint"],
      }],
    });
    getCreativeModelCandidates.mockResolvedValue([{
      group_id: 12,
      group_name: "Gemini Images",
      platform: "gemini",
      model: "gemini-3.1-flash-image",
      operations: ["generate", "edit"],
    }]);

    const wrapper = mountView();
    await flushPromises();
    await wrapper.find("form").trigger("submit");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({ creative_model_settings: [] }),
    );
  });
});

describe("admin SettingsView security tab controls", () => {
  beforeEach(() => {
    getSettings.mockReset();
    getCreativeModelCandidates.mockReset();
    updateSettings.mockReset();
    getWebSearchEmulationConfig.mockReset();
    updateWebSearchEmulationConfig.mockReset();
    getAdminApiKey.mockReset();
    getOverloadCooldownSettings.mockReset();
    getOpenAI403CooldownSettings.mockReset();
    updateOpenAI403CooldownSettings.mockReset();
    getRateLimit429CooldownSettings.mockReset();
    updateRateLimit429CooldownSettings.mockReset();
    getStreamTimeoutSettings.mockReset();
    getRectifierSettings.mockReset();
    getBetaPolicySettings.mockReset();
    getGroups.mockReset();
    listProxies.mockReset();
    getProviders.mockReset();
    updateProvider.mockReset();
    createProvider.mockReset();
    deleteProvider.mockReset();
    fetchPublicSettings.mockReset();
    adminSettingsFetch.mockReset();
    showError.mockReset();
    showSuccess.mockReset();

    getSettings.mockResolvedValue({
      ...baseSettingsResponse,
      payment_visible_method_wxpay_source: "official_wxpay",
    });
    getCreativeModelCandidates.mockResolvedValue([]);
    updateSettings.mockImplementation(async (payload) => ({
      ...baseSettingsResponse,
      payment_visible_method_wxpay_source: "official_wxpay",
      ...payload,
    }));
    getWebSearchEmulationConfig.mockResolvedValue({
      enabled: false,
      providers: [],
    });
    updateWebSearchEmulationConfig.mockResolvedValue({
      enabled: false,
      providers: [],
    });
    getAdminApiKey.mockResolvedValue({
      exists: false,
      masked_key: "",
    });
    getOverloadCooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_minutes: 10,
    });
    getOpenAI403CooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_minutes: 10,
      error_on_threshold_enabled: true,
      threshold_count: 3,
      threshold_window_minutes: 180,
    });
    updateOpenAI403CooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_minutes: 10,
      error_on_threshold_enabled: true,
      threshold_count: 3,
      threshold_window_minutes: 180,
    });
    getRateLimit429CooldownSettings.mockResolvedValue({
      enabled: true,
      cooldown_seconds: 5,
    });
    updateRateLimit429CooldownSettings.mockImplementation(async (payload) => payload);
    getStreamTimeoutSettings.mockResolvedValue({
      enabled: true,
      action: "temp_unsched",
      temp_unsched_minutes: 5,
      threshold_count: 3,
      threshold_window_minutes: 10,
    });
    getRectifierSettings.mockResolvedValue({
      enabled: true,
      thinking_signature_enabled: true,
      thinking_budget_enabled: true,
      apikey_signature_enabled: false,
      apikey_signature_patterns: [],
    });
    getBetaPolicySettings.mockResolvedValue({
      rules: [],
    });
    getGroups.mockResolvedValue([]);
    listProxies.mockResolvedValue({
      items: [],
    });
    getProviders.mockResolvedValue({
      data: [],
    });
    fetchPublicSettings.mockResolvedValue(undefined);
    adminSettingsFetch.mockResolvedValue(undefined);
  });

  it("shows and saves frontend URL when password reset is disabled", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      email_verify_enabled: false,
      password_reset_enabled: false,
      frontend_url: "https://router.example",
    });

    const wrapper = mountView();

    await flushPromises();
    await openSecurityTab(wrapper);

    const input = wrapper.get('[data-testid="frontend-url-input"]');
    expect((input.element as HTMLInputElement).value).toBe("https://router.example");

    await input.setValue("https://new-router.example");
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        frontend_url: "https://new-router.example",
      }),
    );
  });

  it("renders and submits registration email normalization settings", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      registration_email_normalization: true,
    });

    const wrapper = mountView();

    await flushPromises();
    await openSecurityTab(wrapper);

    // 覆盖回归：该开关曾在合并上游后从模板和保存载荷中同时丢失。
    const normalizationSetting = wrapper
      .findAll("div")
      .find((node) => node.text().includes("邮箱地址归一化"));

    expect(normalizationSetting).toBeDefined();
    const normalizationToggle = normalizationSetting?.find("input.toggle-stub");
    expect(normalizationToggle?.exists()).toBe(true);
    expect(
      (normalizationToggle?.element as HTMLInputElement | undefined)?.checked,
    ).toBe(true);

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        registration_email_normalization: true,
      }),
    );
  });

  it("renders and submits the email domain quota switch", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      registration_email_domain_quota_enabled: true,
    });

    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);

    const quotaSetting = wrapper
      .findAll("div")
      .find((node) => node.text().includes("非白名单域名限量注册"));
    expect(quotaSetting).toBeDefined();
    const quotaToggle = quotaSetting?.find("input.toggle-stub");
    expect(quotaToggle?.exists()).toBe(true);
    expect((quotaToggle?.element as HTMLInputElement | undefined)?.checked).toBe(true);

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        registration_email_domain_quota_enabled: true,
      }),
    );
  });

  it("renders and submits the user email change switch", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      user_email_change_enabled: true,
    });

    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);

    const emailChangeSetting = wrapper.get('[data-testid="user-email-change-setting"]');
    const emailChangeToggle = emailChangeSetting.get("input.toggle-stub");
    expect((emailChangeToggle.element as HTMLInputElement).checked).toBe(true);

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        user_email_change_enabled: true,
      }),
    );
  });

  it("shows the save bar only while settings have unsaved changes", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);

    expect(wrapper.find('[data-testid="settings-save-bar"]').exists()).toBe(false);

    await wrapper
      .get('[data-testid="user-email-change-setting"] input.toggle-stub')
      .setValue(true);
    expect(wrapper.find('[data-testid="settings-save-bar"]').exists()).toBe(true);

    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalled();
    // jsdom 不会结束离场动画，这里检查保存条已经进入退出状态。
    expect(
      wrapper.get('[data-testid="settings-save-bar"]').attributes("inert"),
    ).toBeDefined();
  });

  it("reloads changed settings from the server when discarding", async () => {
    const wrapper = mountView();
    await flushPromises();
    await openSecurityTab(wrapper);
    const loadsBefore = getSettings.mock.calls.length;

    await wrapper
      .get('[data-testid="user-email-change-setting"] input.toggle-stub')
      .setValue(true);
    await wrapper.get('[data-testid="settings-save-bar-discard"]').trigger("click");
    await flushPromises();

    expect(getSettings.mock.calls.length).toBe(loadsBefore + 1);
    expect(updateSettings).not.toHaveBeenCalled();
    expect(
      (wrapper.get('[data-testid="user-email-change-setting"] input.toggle-stub')
        .element as HTMLInputElement).checked,
    ).toBe(false);
  });

  it("loads and echoes WeChat Connect fields from the backend payload", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openSecurityTab(wrapper);

    expect(
      (
        wrapper.get('[data-testid="wechat-connect-mp-app-id"]')
          .element as HTMLInputElement
      ).value,
    ).toBe("wx-app-id-123");
    expect(
      (
        wrapper.get('[data-testid="wechat-connect-open-enabled"]')
          .element as HTMLInputElement
      ).checked,
    ).toBe(false);
    expect(
      (
        wrapper.get('[data-testid="wechat-connect-mp-enabled"]')
          .element as HTMLInputElement
      ).checked,
    ).toBe(true);
    expect(wrapper.find('[data-testid="wechat-connect-scopes"]').exists()).toBe(
      false,
    );
    expect(
      wrapper
        .get('[data-testid="wechat-connect-mp-app-secret"]')
        .attributes("placeholder"),
    ).toContain("密钥已配置");
    expect(
      (
        wrapper.get('[data-testid="wechat-connect-frontend-redirect-url"]')
          .element as HTMLInputElement
      ).value,
    ).toBe("/auth/wechat/callback");
  });

  it("links GitHub OAuth Apps guide to GitHub developer settings", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      github_oauth_enabled: true,
    });

    const wrapper = mountView();

    await flushPromises();
    await openSecurityTab(wrapper);

    const link = wrapper.get('[data-testid="github-oauth-apps-guide-link"]');
    expect(link.text()).toContain("OAuth Apps");
    expect(link.attributes("href")).toBe("https://github.com/settings/developers");
    expect(link.attributes("target")).toBe("_blank");
    expect(link.attributes("rel")).toContain("noopener");
  });

  it("saves WeChat Connect fields using the backend contract and clears the secret after save", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openSecurityTab(wrapper);

    await wrapper
      .get('[data-testid="wechat-connect-mp-app-id"]')
      .setValue("wx-app-id-updated");
    await wrapper
      .get('[data-testid="wechat-connect-mp-app-secret"]')
      .setValue("new-secret");
    await wrapper
      .get('[data-testid="wechat-connect-open-enabled"]')
      .setValue(true);
    await wrapper
      .get('[data-testid="wechat-connect-mp-enabled"]')
      .setValue(true);
    await wrapper
      .get('[data-testid="wechat-connect-redirect-url"]')
      .setValue("https://admin.example.com/api/v1/auth/oauth/wechat/callback");
    await wrapper
      .get('[data-testid="wechat-connect-frontend-redirect-url"]')
      .setValue("/auth/wechat/callback");
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        wechat_connect_enabled: true,
        wechat_connect_app_id: "wx-app-id-updated",
        wechat_connect_open_enabled: true,
        wechat_connect_mp_enabled: true,
        wechat_connect_mp_app_id: "wx-app-id-updated",
        wechat_connect_mp_app_secret: "new-secret",
        wechat_connect_redirect_url:
          "https://admin.example.com/api/v1/auth/oauth/wechat/callback",
        wechat_connect_frontend_redirect_url: "/auth/wechat/callback",
      }),
    );
    expect(
      (
        wrapper.get('[data-testid="wechat-connect-mp-app-secret"]')
          .element as HTMLInputElement
      ).value,
    ).toBe("");
    expect(
      wrapper
        .get('[data-testid="wechat-connect-mp-app-secret"]')
        .attributes("placeholder"),
    ).toContain("密钥已配置");
  });

  it("collapses auth source defaults until the source is enabled", async () => {
    const wrapper = mountView();

    await flushPromises();
    await openUsersTab(wrapper);

    expect(
      (
        wrapper.get('[data-testid="auth-source-email-enabled"]')
          .element as HTMLInputElement
      ).checked,
    ).toBe(false);
    expect(
      wrapper.find('[data-testid="auth-source-email-panel"]').exists(),
    ).toBe(false);
    expect(wrapper.text()).not.toContain("注册即授权");

    await wrapper
      .get('[data-testid="auth-source-email-enabled"]')
      .setValue(true);

    expect(
      wrapper.find('[data-testid="auth-source-email-panel"]').exists(),
    ).toBe(true);
    expect(wrapper.text()).toContain("首次绑定时授权");
  });

  it("preserves optional OIDC compatibility flags instead of forcing them on save", async () => {
    getSettings.mockResolvedValueOnce({
      ...baseSettingsResponse,
      oidc_connect_enabled: true,
      oidc_connect_use_pkce: false,
      oidc_connect_validate_id_token: false,
    });

    const wrapper = mountView();

    await flushPromises();
    await openSecurityTab(wrapper);
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();

    expect(updateSettings).toHaveBeenCalledTimes(1);
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        oidc_connect_use_pkce: false,
        oidc_connect_validate_id_token: false,
      }),
    );
  });
});
