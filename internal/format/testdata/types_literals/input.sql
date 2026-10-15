create table types (
  a int4, b integer, c int, d smallint, e int8, f bool, g boolean,
  h varchar(255), i character varying, j char(3), k numeric, l decimal(10, 2),
  m timestamp, n timestamp(3) with time zone, o time with time zone, p double precision,
  q real, r float8, s uuid, t jsonb, u text[], v int[3], w "MyType", x bpchar,
  y text default E'line1\nline2', z text default 'it''s', aa text default 'a\b',
  bb int default -1, cc int default (1 + 2) * 3, dd bool default (not (true and false)),
  ee text default 'x' || 'y', ff int default - (1 + 2), gg timestamptz default current_timestamp,
  hh timestamptz default ('2026-01-01'::timestamptz at time zone 'UTC'), ii numeric default 1.5,
  jj int default coalesce(null, 1), kk text default cast(1 as text) collate "C",
  "UserId" int, userId int
);
