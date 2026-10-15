CREATE TYPE "order_status" AS ENUM (
    'new',
    'pending',
    'shipped'
);

ALTER TYPE "order_status" ADD VALUE IF NOT EXISTS 'returned' AFTER 'shipped';

CREATE OR REPLACE VIEW "recent_orcamentos" AS
SELECT "id",
       "email"
  FROM "site_orcamentos";

CREATE SEQUENCE IF NOT EXISTS "invoice_seq"
START WITH 1000
INCREMENT BY 1
NO MAXVALUE
CYCLE
OWNED BY "site_orcamentos"."id";

CREATE SCHEMA IF NOT EXISTS "billing";

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA "public";

COMMENT ON COLUMN "site_orcamentos"."gclid" IS 'Google click id';

COMMENT ON TABLE "site_orcamentos" IS NULL;

GRANT SELECT, INSERT ON "site_orcamentos" TO "app_user";

GRANT USAGE ON SCHEMA "billing" TO PUBLIC;

REVOKE ALL ON ALL TABLES IN SCHEMA "billing" FROM "app_user" CASCADE;

GRANT EXECUTE ON FUNCTION "f"(INTEGER) TO "app_user" WITH GRANT OPTION;

ALTER TABLE "site_orcamentos"
RENAME COLUMN "gclid" TO "google_click_id";

ALTER TABLE IF EXISTS "a"
RENAME TO "b";
