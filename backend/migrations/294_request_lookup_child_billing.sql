-- 父请求查询包含关联子记录的计费键与历史引用，费用仍按子记录的 API Key 匹配。
CREATE OR REPLACE FUNCTION request_lookup_ids(search_id TEXT) RETURNS TABLE(id TEXT)
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
), related_ids AS (
    SELECT relation->>'value' AS request_id FROM matched
    CROSS JOIN LATERAL jsonb_array_elements(COALESCE(record->'aliases','[]'::jsonb)) relation
    WHERE relation->>'kind'='related'
    UNION
    SELECT child.request_id FROM matched m JOIN request_records child ON child.parent_request_id=m.request_id
    WHERE child.parent_request_id <> ''
), expanded AS (
    SELECT request_id, aliases, record FROM matched
    UNION
    SELECT child.request_id, child.aliases, child.record
    FROM related_ids related JOIN request_records child ON child.request_id=related.request_id
)
SELECT value FROM candidates WHERE value IS NOT NULL AND value <> ''
UNION SELECT request_id FROM matched
UNION SELECT u.request_id FROM expanded m
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(m.record->'aliases','[]'::jsonb)) a
JOIN usage_logs u ON COALESCE(u.billing_key,u.request_id)=a->>'value'
 AND u.api_key_id=(m.record->>'api_key_id')::bigint
WHERE a->>'kind'='billing'
UNION SELECT a->>'value' FROM expanded m
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(m.record->'aliases','[]'::jsonb)) a
WHERE a->>'kind'='legacy'
UNION SELECT request_id FROM related_ids
$$;
