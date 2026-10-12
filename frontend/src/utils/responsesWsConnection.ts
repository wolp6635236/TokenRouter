export const RESPONSES_WS_POOLED = 'pooled'
export const RESPONSES_WS_PER_SESSION = 'per_session'
export type ResponsesWSConnectionMode = typeof RESPONSES_WS_POOLED | typeof RESPONSES_WS_PER_SESSION

// readResponsesWSConnectionMode 读取服务端规范化后的连接方式。
export function readResponsesWSConnectionMode(extra: Record<string, unknown> | null | undefined): ResponsesWSConnectionMode {
  return extra?.responses_ws_connection_mode === RESPONSES_WS_PER_SESSION ? RESPONSES_WS_PER_SESSION : RESPONSES_WS_POOLED
}

// responsesWSConnectionHint 返回当前连接方式的说明词条。
export function responsesWSConnectionHint(mode: ResponsesWSConnectionMode) {
  return mode === RESPONSES_WS_PER_SESSION
    ? 'admin.providers.openai.wsConnectionPerSessionHint'
    : 'admin.providers.openai.wsConnectionPooledHint'
}

// clearLegacyResponsesWSSettings 删除已转换的提供商设置。
export function clearLegacyResponsesWSSettings(extra: Record<string, unknown>) {
  for (const key of ['openai_oauth_responses_websockets_v2_mode', 'openai_apikey_responses_websockets_v2_mode', 'openai_oauth_responses_websockets_v2_enabled', 'openai_apikey_responses_websockets_v2_enabled', 'responses_websockets_v2_enabled', 'openai_ws_enabled', 'openai_ws_force_http']) delete extra[key]
}
