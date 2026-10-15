CREATE OR REPLACE FUNCTION "f"(
    "p_since"       TIMESTAMPTZ
)
RETURNS BIGINT AS $$
declare
    -- counters
    v_count bigint;
    v_last_created_at timestamptz; -- newest row
    v_source text default 'ads';
begin
    -- count rows
    select count(*) into v_count from site_orcamentos where created_at > p_since;
    if tg_op = 'INSERT' then
        new.created_at := now();
    elsif tg_op = 'UPDATE' then
        null;
    else
        raise exception 'bad op %', tg_op;
    end if;
    for i in 1..10 loop
        exit when i > v_count;
    end loop;
    for r in select id from site_orcamentos loop
        perform pg_notify('x', r.id::text);
    end loop;
    begin
        insert into t (a) values (1) returning id into v_count;
    exception when unique_violation then
        v_count = 0;
    end;
    return v_count;
end;
$$ LANGUAGE plpgsql;
