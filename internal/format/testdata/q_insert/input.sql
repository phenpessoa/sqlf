insert into site_orcamentos (name, email, phone) values ('Ana', 'ana@x.com', '11'), ('Roberto', 'rob@x.com', '2222') on conflict (email) do nothing returning id;
insert into archive (id, name) select id, name from site_orcamentos;
insert into t (a, b) values (1, 2) on conflict (a) do update set b = excluded.b, a = 3 where t.b <> excluded.b;
insert into t default values;
