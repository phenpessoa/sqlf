INSERT INTO "site_orcamentos" (
    "name",
    "email",
    "phone"
)
VALUES ('Ana', 'ana@x.com', '11'),
       ('Roberto', 'rob@x.com', '2222')
    ON CONFLICT ("email") DO NOTHING
RETURNING "id";

INSERT INTO "archive" (
    "id",
    "name"
)
SELECT "id",
       "name"
  FROM "site_orcamentos";

INSERT INTO "t" (
    "a",
    "b"
)
VALUES (1, 2)
    ON CONFLICT ("a") DO UPDATE
   SET "b"      = EXCLUDED."b",
       "a"      = 3
 WHERE "t"."b" <> EXCLUDED."b";

INSERT INTO "t"
DEFAULT VALUES;
