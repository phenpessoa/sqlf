UPDATE "site_orcamentos"
   SET "name"           = 'Ana',
       "email"          = 'ana@x.com',
       "updated_at"     = NOW()
 WHERE "id" = 1;

UPDATE "site_orcamentos"
   SET "name"       = 'Ana'
 WHERE "id" = 1
RETURNING "id", "name";
