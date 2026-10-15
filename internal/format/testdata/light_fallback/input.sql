create policy orcamentos_read on site_orcamentos for select to app_user using (true);
select id,email   from site_orcamentos
  where id != 1;
select id, -- the key
  email from t where a != b;
