CREATE POLICY orcamentos_read ON site_orcamentos FOR SELECT TO app_user USING (TRUE);

SELECT "id",
       "email"
  FROM "site_orcamentos"
 WHERE "id" <> 1;

SELECT id, -- the key
  email FROM t WHERE a <> b;
