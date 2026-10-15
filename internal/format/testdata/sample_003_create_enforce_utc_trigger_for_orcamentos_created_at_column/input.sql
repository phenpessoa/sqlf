CREATE TRIGGER enforce_utc_for_created_at_on_site_orcamentos_trigger
BEFORE INSERT OR UPDATE ON site_orcamentos
FOR EACH ROW
EXECUTE FUNCTION enforce_utc_for_created_at();

---- create above / drop below ----

DROP TRIGGER IF EXISTS enforce_utc_for_created_at_on_orcamentos_trigger ON site_orcamentos;
