-- file header

-- about a
CREATE TABLE "a" (
    "id"    INTEGER
);

DROP TABLE "b"; -- inline after b

-- glued to both: joins c
CREATE TABLE "c" (
    "id"    INTEGER
);
-- trailing c

-- standalone
/* block
   comment */

DROP TABLE "d";
