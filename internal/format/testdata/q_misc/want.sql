SELECT DISTINCT "a",
       "b"
  FROM "t";

SELECT *
  FROM "t"
   FOR UPDATE SKIP LOCKED;

SELECT "a"
  FROM "t"
 CROSS JOIN "u"
  JOIN "v"
 USING ("id");

SELECT SUM("x") FILTER (WHERE "x" > 0),
       STRING_AGG("name", ', ' ORDER BY "name")
  FROM "t";

SELECT *
  FROM CAST(1 AS TEXT);
