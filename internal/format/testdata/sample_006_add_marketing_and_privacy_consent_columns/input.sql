ALTER TABLE site_orcamentos
ADD COLUMN IF NOT EXISTS privacy_consent   BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS marketing_consent BOOLEAN NOT NULL DEFAULT FALSE;

---- create above / drop below ----

ALTER TABLE site_orcamentos
DROP COLUMN IF EXISTS privacy_consent,
DROP COLUMN IF EXISTS marketing_consent;
