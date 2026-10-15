SELECT "first_name" || ' ' || "middle_name" || ' ' || "last_name" || ' <' || "email" || '>' || ' (' || "company_name"
           || ')' AS "display"
  FROM "contacts";

SELECT 1
  FROM "o"
 WHERE "o"."status" IN (
           'new', 'pending', 'review', 'approved', 'shipped', 'delivered', 'returned', 'cancelled', 'refunded', 'lost'
       );
