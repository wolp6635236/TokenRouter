-- 资金去重键独立保存，历史记录继续以已有 request_id 去重。
ALTER TABLE usage_logs ADD COLUMN billing_key VARCHAR(255);

-- 请求摘要独立于 Ops 采样保存；别名用于兼容调用方、供应商和旧计费标识。
CREATE TABLE request_records (
    request_id VARCHAR(64) PRIMARY KEY,
    parent_request_id VARCHAR(64) NOT NULL DEFAULT '',
    user_id BIGINT NOT NULL DEFAULT 0,
    team_id BIGINT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    revision BIGINT NOT NULL,
    aliases TEXT[] NOT NULL DEFAULT '{}',
    record JSONB NOT NULL
);
CREATE INDEX request_records_started_at_idx ON request_records(started_at);
CREATE INDEX request_records_parent_idx ON request_records(parent_request_id) WHERE parent_request_id <> '';
CREATE INDEX request_records_aliases_idx ON request_records USING GIN(aliases);

-- 使用记录按计费键和 API Key 去重，已有记录使用 request_id。
CREATE UNIQUE INDEX idx_usage_logs_billing_key_api_key
ON usage_logs ((COALESCE(billing_key, request_id)), api_key_id);

-- 请求 ID 的每列查询分别使用等值关联，历史日志通过已有 B-tree 索引定位。
CREATE FUNCTION request_lookup_ids(search_id TEXT) RETURNS TABLE(id TEXT)
LANGUAGE SQL STABLE AS $$
WITH exact AS MATERIALIZED (
    SELECT request_id, aliases, record FROM request_records WHERE request_id=btrim(search_id)
), base AS (
    SELECT btrim(search_id) AS value WHERE NOT EXISTS (SELECT 1 FROM exact)
    UNION SELECT regexp_replace(btrim(search_id), '^(client:|local:|generated:)', '') WHERE NOT EXISTS (SELECT 1 FROM exact)
), variants AS (
    SELECT value FROM base
    UNION SELECT 'client:' || value FROM base
    UNION SELECT 'local:' || value FROM base
), legacy AS (
    SELECT o.request_id, o.client_request_id
    FROM variants v JOIN ops_error_logs o ON o.request_id=v.value
    UNION
    SELECT o.request_id, o.client_request_id
    FROM variants v JOIN ops_error_logs o ON o.client_request_id=v.value
    UNION
    SELECT l.request_id, l.client_request_id
    FROM variants v JOIN ops_system_logs l ON l.request_id=v.value
    UNION
    SELECT l.request_id, l.client_request_id
    FROM variants v JOIN ops_system_logs l ON l.client_request_id=v.value
), candidates AS (
    SELECT value FROM variants
    UNION SELECT u.request_id FROM variants v JOIN usage_logs u ON COALESCE(u.billing_key, u.request_id)=v.value
    UNION SELECT u.request_id FROM variants v JOIN usage_logs u ON u.upstream_request_id=v.value
    UNION SELECT request_id FROM legacy
    UNION SELECT client_request_id FROM legacy
    UNION SELECT 'client:' || client_request_id FROM legacy
), matched AS (
    SELECT request_id, aliases, record FROM exact
    UNION
    SELECT r.request_id, r.aliases, r.record
    FROM candidates c JOIN request_records r ON r.request_id=c.value
    UNION
    SELECT r.request_id, r.aliases, r.record FROM request_records r
    WHERE NOT EXISTS (SELECT 1 FROM exact)
      AND r.aliases && ARRAY(SELECT value FROM candidates)
)
SELECT value FROM candidates WHERE value IS NOT NULL AND value <> ''
UNION SELECT request_id FROM matched
UNION SELECT u.request_id FROM matched m
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(m.record->'aliases','[]'::jsonb)) a
JOIN usage_logs u ON COALESCE(u.billing_key,u.request_id)=a->>'value'
 AND u.api_key_id=(m.record->>'api_key_id')::bigint
WHERE a->>'kind'='billing'
UNION SELECT a->>'value' FROM matched m
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(m.record->'aliases','[]'::jsonb)) a
WHERE a->>'kind'='legacy'
UNION SELECT relation->>'value' FROM matched CROSS JOIN LATERAL jsonb_array_elements(COALESCE(record->'aliases','[]'::jsonb)) relation WHERE relation->>'kind'='related'
UNION SELECT child.request_id FROM matched m JOIN request_records child ON child.parent_request_id=m.request_id
WHERE child.parent_request_id <> ''
$$;
