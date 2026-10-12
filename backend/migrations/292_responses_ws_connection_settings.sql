-- 将提供商的 Responses WS 连接方式统一保存，客户端许可由分组控制。
WITH source AS (
 SELECT id, extra,
  CASE WHEN type = 'apikey' THEN extra->>'openai_apikey_responses_websockets_v2_mode'
       ELSE extra->>'openai_oauth_responses_websockets_v2_mode' END AS old_mode
 FROM providers WHERE platform = 'openai'
), converted AS (
 SELECT id,
  (extra - 'openai_oauth_responses_websockets_v2_mode' - 'openai_apikey_responses_websockets_v2_mode'
   - 'openai_oauth_responses_websockets_v2_enabled' - 'openai_apikey_responses_websockets_v2_enabled'
   - 'responses_websockets_v2_enabled' - 'openai_ws_enabled' - 'openai_ws_force_http') AS clean_extra,
  CASE WHEN extra ? 'responses_ws_connection_mode' THEN extra->>'responses_ws_connection_mode'
       WHEN lower(trim(old_mode)) = 'passthrough' THEN 'per_session' ELSE 'pooled' END AS mode,
  NOT (extra ? 'responses_ws_connection_mode') AND
   (lower(trim(old_mode)) = 'http_bridge' OR extra->'openai_ws_force_http' = 'true'::jsonb) AS use_http
 FROM source
)
UPDATE providers p SET
 extra = COALESCE(c.clean_extra, '{}'::jsonb) || jsonb_build_object('responses_ws_connection_mode', c.mode),
 credentials = CASE WHEN c.use_http AND jsonb_typeof(p.credentials->'upstream_protocols') = 'array'
  THEN jsonb_set(p.credentials, '{upstream_protocols}',
   COALESCE((SELECT jsonb_agg(item) FROM jsonb_array_elements(p.credentials->'upstream_protocols') item
     WHERE item <> '"openai_responses_websocket"'::jsonb), '[]'::jsonb))
  ELSE p.credentials END,
 updated_at = NOW()
FROM converted c WHERE p.id = c.id;
