create table t (
  -- identity
  id bigserial primary key not null, -- surrogate key
  email text not null, -- login
  note text
  -- end of columns
);

alter table t
  add column a text, -- first
  -- before b
  add column bb integer not null default 0;
