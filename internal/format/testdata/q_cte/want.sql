WITH "recent" AS (
    SELECT "id",
           "email"
      FROM "site_orcamentos"
     WHERE "created_at" > NOW() - '7 days'::INTERVAL
),
"counts" AS (
    SELECT "email",
           COUNT(*)     AS "total"
      FROM "recent"
     GROUP BY "email"
)
SELECT "c"."email",
       "c"."total"
  FROM "counts"     AS "c";
