select distinct a, b from t;
select * from t for update skip locked;
select a from t cross join u join v using (id);
select sum(x) filter (where x > 0), string_agg(name, ', ' order by name) from t;
select * from cast(1 as text);
