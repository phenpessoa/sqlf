ALTER TABLE "whatsapp_tracking"
ADD COLUMN IF NOT EXISTS        "gclid"                     TEXT                                        NOT NULL    DEFAULT '',
ADD COLUMN                      "utm_campaign_id"           BIGINT,
DROP COLUMN IF EXISTS           "keyword",
DROP COLUMN                     "legacy_source" CASCADE,
ALTER COLUMN                    "page"                      SET NOT NULL,
ALTER COLUMN                    "page"                      TYPE VARCHAR(255) USING "page"::VARCHAR,
ALTER COLUMN                    "hash"                      SET DEFAULT MD5(''),
ALTER COLUMN                    "hash"                      DROP DEFAULT,
ADD CONSTRAINT                  "wt_hash_key"               UNIQUE ("hash"),
DROP CONSTRAINT IF EXISTS       "old_key";

ALTER TABLE ONLY "site_orcamentos"
ENABLE ROW LEVEL SECURITY;
