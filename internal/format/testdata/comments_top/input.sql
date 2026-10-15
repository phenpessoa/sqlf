-- file header

-- about a
create table a (id int);
drop table b; -- inline after b
-- glued to both: joins c
create table c (id int);
-- trailing c


-- standalone
/* block
   comment */


drop table d;
