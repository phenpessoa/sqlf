CREATE FUNCTION "count_orcamentos"(
    "p_since"       TIMESTAMPTZ,
    "p_source"      TEXT,
    "p_limit"       INTEGER         DEFAULT 10
)
RETURNS BIGINT AS $$
BEGIN
    RETURN 1;
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

CREATE PROCEDURE "p"(
    INOUT       "x"     INTEGER,
    VARIADIC    "ys"    INTEGER[]
)
AS $$
SELECT 1;
$$ LANGUAGE sql;
