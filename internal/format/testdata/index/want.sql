CREATE UNIQUE INDEX IF NOT EXISTS "site_orcamentos_email_idx"
ON "site_orcamentos" ("email")
WHERE "deleted_at" IS NULL;

CREATE INDEX "wt_created_at_idx"
ON "whatsapp_tracking" USING brin ("created_at");

CREATE INDEX CONCURRENTLY
ON "t" (LOWER("email"), ("a" + "b") DESC NULLS LAST)
INCLUDE ("c");
