ALTER TABLE "whatsapp_tracking"
ADD COLUMN IF NOT EXISTS    "gclid"     TEXT    NOT NULL    DEFAULT '';

---- create above / drop below ----

ALTER TABLE "whatsapp_tracking"
DROP COLUMN IF EXISTS       "gclid";
