create function count_orcamentos(p_since timestamptz, p_source text, p_limit int default 10) returns bigint
language plpgsql stable security definer as $$
begin
    return 1;
end;
$$;
create procedure p(inout x int, variadic ys int[]) language sql as $body$ select 1 $body$;
