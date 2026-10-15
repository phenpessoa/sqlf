SELECT 1
  FROM "t"
 WHERE "source" = 'ads'
   AND (
           "page" = '/home'
        OR "page" = '/contato'
       )
    OR "id" = 1;

SELECT 1
  FROM "t"
 WHERE "id" IN (
           SELECT "x"
             FROM "y"
       )
   AND NOT EXISTS (
           SELECT 1
             FROM "z"
            WHERE "z"."id" = "t"."id"
       );
