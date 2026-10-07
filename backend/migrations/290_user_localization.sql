-- 用户主动选择的语言供界面和后台通知共用，历史账户从通知偏好迁入。
ALTER TABLE users ADD COLUMN IF NOT EXISTS preferred_locale VARCHAR(35);
UPDATE users u SET preferred_locale = CASE s.value
    WHEN 'zh' THEN 'zh-Hans'
    WHEN 'zh-Hans' THEN 'zh-Hans'
    WHEN 'en' THEN 'en'
END
FROM settings s
WHERE s.key = 'notification_email_locale:user:' || u.id::text
  AND s.value IN ('zh', 'zh-Hans', 'en') AND u.preferred_locale IS NULL;

-- 站点文案保存原文和译文；空标题与空副标题使用内置文案，导入时跳过。
WITH fields(name, fallback) AS (
    VALUES ('site_name', 'TokenRouter'), ('site_title', ''), ('site_subtitle', ''),
           ('contact_info', ''), ('doc_url', ''), ('home_content', ''),
           ('purchase_subscription_url', ''), ('footer_text', '')
), copies AS (
    SELECT f.name,
           COALESCE(
               CASE WHEN b.value !~ '^[[:space:]]*$' THEN b.value END,
               CASE WHEN z.value !~ '^[[:space:]]*$' THEN z.value END,
               CASE WHEN e.value !~ '^[[:space:]]*$' THEN e.value END,
               f.fallback
           ) AS original,
           z.value AS zh, e.value AS en,
           (b.key IS NOT NULL OR z.key IS NOT NULL OR e.key IS NOT NULL) AS configured
    FROM fields f
    LEFT JOIN settings b ON b.key = f.name
    LEFT JOIN settings z ON z.key = f.name || '_zh'
    LEFT JOIN settings e ON e.key = f.name || '_en'
), payload AS (
    SELECT jsonb_object_agg(name, jsonb_build_object(
        'source_locale', NULL, 'source', original, 'revision', 1, 'source_revision', 1,
        'translations',
            CASE WHEN COALESCE(zh, '') !~ '^[[:space:]]*$' THEN jsonb_build_object('zh-Hans', jsonb_build_object('value', zh, 'source_revision', 1)) ELSE '{}'::jsonb END ||
            CASE WHEN COALESCE(en, '') !~ '^[[:space:]]*$' THEN jsonb_build_object('en', jsonb_build_object('value', en, 'source_revision', 1)) ELSE '{}'::jsonb END
    )) FILTER (WHERE configured AND (name NOT IN ('site_title', 'site_subtitle') OR original <> '')) AS value FROM copies
)
INSERT INTO settings (key, value, updated_at)
SELECT 'site_texts', COALESCE(value, '{}'::jsonb)::text, now() FROM payload
ON CONFLICT (key) DO NOTHING;

INSERT INTO settings (key, value, updated_at) VALUES ('default_locale', 'en', now()) ON CONFLICT (key) DO NOTHING;
DELETE FROM settings WHERE key IN ('site_name_zh', 'site_name_en', 'site_title_zh', 'site_title_en', 'site_subtitle_zh', 'site_subtitle_en');

-- 公告的所有语言共用同一条公告和已读记录。
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS localization JSONB;
UPDATE announcements SET localization = jsonb_build_object(
    'source_locale', NULL, 'source', jsonb_build_object('title', title, 'content', content),
    'translations', '{}'::jsonb, 'revision', 1, 'source_revision', 1
) WHERE localization IS NULL;

-- 普通用户可见的独立文案保存原文和译文，内部字段继续使用现有格式。
WITH fields(key, fallback) AS (
    VALUES ('balance_unit_name', 'USD'), ('balance_low_notify_recharge_url', ''),
           ('oidc_connect_provider_name', ''), ('PAYMENT_HELP_TEXT', ''),
           ('PAYMENT_HELP_IMAGE_URL', ''), ('PRODUCT_NAME_PREFIX', ''),
           ('PRODUCT_NAME_SUFFIX', ''), ('smtp_from_name', '')
)
INSERT INTO settings(key, value, updated_at)
SELECT f.key || '_localized', jsonb_build_object(
    'source_locale', NULL, 'source', COALESCE(NULLIF(s.value, ''), f.fallback),
    'translations', '{}'::jsonb, 'revision', 1, 'source_revision', 1
)::text, now()
FROM fields f LEFT JOIN settings s ON s.key = f.key
ON CONFLICT(key) DO NOTHING;

-- 将历史中文模板与邮箱偏好迁入共用语言代码。
INSERT INTO settings(key, value, updated_at)
SELECT regexp_replace(key, ':zh$', ':zh-Hans'), value, updated_at
FROM settings WHERE key LIKE 'notification_email_template:%:zh'
ON CONFLICT(key) DO NOTHING;
DELETE FROM settings WHERE key LIKE 'notification_email_template:%:zh';
UPDATE settings SET value = 'zh-Hans'
WHERE key LIKE 'notification_email_locale:%' AND value = 'zh';

-- 套餐译文与价格和权益存放在同一业务对象中。
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS localization JSONB;
UPDATE subscription_plans SET localization = jsonb_build_object(
    'source_locale', NULL,
    'source', jsonb_build_object('name', name, 'description', description, 'features', features, 'product_name', product_name),
    'translations', '{}'::jsonb, 'revision', 1, 'source_revision', 1
) WHERE localization IS NULL;

-- 用户展示名称独立于分组的唯一业务名称。
ALTER TABLE groups ADD COLUMN IF NOT EXISTS localization JSONB;
UPDATE groups SET localization = jsonb_build_object(
    'source_locale', NULL,
    'source', jsonb_build_object('display_name', name, 'description', COALESCE(description, '')),
    'translations', '{}'::jsonb, 'revision', 1, 'source_revision', 1
) WHERE localization IS NULL;
