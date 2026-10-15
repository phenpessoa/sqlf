DELETE FROM "whatsapp_tracking"
 USING "site_orcamentos"    AS "o"
 WHERE "whatsapp_tracking"."page" = "o"."email";

SELECT "id",
       ROW_NUMBER() OVER (PARTITION BY "source" ORDER BY "created_at" DESC)     AS "rn"
  FROM "whatsapp_tracking"
 LIMIT 10
OFFSET 20;

SELECT "email"
  FROM "a"
UNION ALL
SELECT "email"
  FROM "b";
