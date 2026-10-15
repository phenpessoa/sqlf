SELECT "o"."id"     AS "order_id",
       COUNT(*)     AS "total",
       "w"."page"
  FROM "site_orcamentos"            AS "o"
  LEFT JOIN "whatsapp_tracking"     AS "w"
    ON "w"."page" = "o"."email"
   AND "w"."source" = 'ads'
 WHERE "o"."id" > 10
 GROUP BY "o"."id", "w"."page"
 ORDER BY "o"."id" DESC;
