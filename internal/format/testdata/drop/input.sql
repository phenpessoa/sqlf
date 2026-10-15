drop table if exists a, s.b cascade;
drop index concurrently if exists i;
drop type if exists order_status;
drop schema billing;
drop extension if exists "uuid-ossp", pgcrypto;
drop function f(int, text), g;
drop trigger if exists tr on s.t;
