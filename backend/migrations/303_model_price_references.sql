-- Model price references retain a separate catalog lookup identity per card.
SET LOCAL lock_timeout = '3s';
ALTER TABLE pricing_config_model_pricing
    ADD COLUMN reference_model TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN pricing_config_model_pricing.reference_model IS 'Same-profile model whose effective prices this card uses';
