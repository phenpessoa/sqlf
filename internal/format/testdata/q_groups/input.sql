select 1 from t where source = 'ads' and (page = '/home' or page = '/contato') or id = 1;
select 1 from t where id in (select x from y) and not exists (select 1 from z where z.id = t.id);
