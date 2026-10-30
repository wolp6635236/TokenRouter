-- Internal model work has no customer funding identity. Stop old binaries
-- before admitting system records because their scanners require these IDs.
ALTER TABLE usage_logs
    ADD COLUMN request_source VARCHAR(32) NOT NULL DEFAULT 'user',
    ADD COLUMN system_usage JSONB,
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN billing_user_id DROP NOT NULL,
    ALTER COLUMN api_key_id DROP NOT NULL;
-- Historical rows already have nonnull funding IDs, the constant user source,
-- and null system metadata. Enforce new writes without rescanning that history;
-- VALIDATE CONSTRAINT can run separately if a validated catalog flag is needed.
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_source_attribution_check CHECK (
    (request_source = 'user' AND user_id IS NOT NULL AND billing_user_id IS NOT NULL AND api_key_id IS NOT NULL AND system_usage IS NULL)
    OR (request_source IN ('group_probe', 'provider_test', 'scheduled_test', 'quota_probe')
        AND user_id IS NULL AND billing_user_id IS NULL AND api_key_id IS NULL
        AND team_id IS NULL AND subscription_id IS NULL AND actual_cost = 0
        AND subscription_amount_usd = 0 AND balance_amount_usd = 0 AND system_usage IS NOT NULL)
) NOT VALID;
CREATE UNIQUE INDEX idx_usage_logs_system_request ON usage_logs (request_id) WHERE request_source <> 'user';
-- These indexes still scan the heap, but exclude all historical customer rows
-- from sorting and storage. Customer time queries retain their existing indexes.
CREATE INDEX idx_usage_logs_system_parent ON usage_logs ((system_usage->>'parent_request_id'))
    WHERE system_usage IS NOT NULL;
CREATE INDEX idx_usage_logs_source_created ON usage_logs (request_source, created_at DESC)
    WHERE request_source <> 'user';

-- This ledger survives analytical cleanup, preventing a replay from charging
-- provider spending again after its usage row has been deliberately removed.
CREATE TABLE billing_system_usage_entries (
    request_id VARCHAR(64) PRIMARY KEY,
    provider_id BIGINT NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    cost_usd NUMERIC(20,10),
    evidence JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX billing_system_usage_entries_provider_time ON billing_system_usage_entries (provider_id, created_at);

CREATE OR REPLACE FUNCTION usage_insert_user_activity()
RETURNS TRIGGER LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    PERFORM usage_lock_user_activity(ARRAY(SELECT DISTINCT user_id FROM new_usage_rows WHERE user_id IS NOT NULL ORDER BY user_id));
    INSERT INTO usage_user_activity (user_id, last_used_at)
    SELECT user_id, MAX(created_at) FROM new_usage_rows WHERE user_id IS NOT NULL GROUP BY user_id ORDER BY user_id
    ON CONFLICT (user_id) DO UPDATE SET last_used_at = EXCLUDED.last_used_at
    WHERE usage_user_activity.last_used_at < EXCLUDED.last_used_at;
    RETURN NULL;
END
$$;

-- Existing buckets describe customer traffic and retain their original totals.
ALTER TABLE usage_analytics_hourly ADD COLUMN request_source VARCHAR(32) NOT NULL DEFAULT 'user';
ALTER TABLE usage_analytics_hourly ADD COLUMN incomplete_cost_requests BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_analytics_hourly DROP CONSTRAINT usage_analytics_hourly_pkey;
ALTER TABLE usage_analytics_hourly ADD PRIMARY KEY (
    bucket_start, user_id, billing_user_id, team_id, api_key_id, group_id,
    requested_model, request_type, stream, billing_type, billing_mode,
    platform, inbound_endpoint, request_source
);
-- Existing buckets describe customer traffic and retain their original totals.
ALTER TABLE usage_analytics_daily ADD COLUMN request_source VARCHAR(32) NOT NULL DEFAULT 'user';
ALTER TABLE usage_analytics_daily ADD COLUMN incomplete_cost_requests BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_analytics_daily DROP CONSTRAINT usage_analytics_daily_pkey;
ALTER TABLE usage_analytics_daily ADD PRIMARY KEY (
    bucket_date, user_id, billing_user_id, team_id, api_key_id, group_id,
    requested_model, request_type, stream, billing_type, billing_mode,
    platform, inbound_endpoint, request_source
);
