CREATE OR REPLACE FUNCTION "f"(
    "p_since"       TIMESTAMPTZ
)
RETURNS TRIGGER AS $$
DECLARE
    -- counters
    "v_count"               BIGINT;
    "v_last_created_at"     TIMESTAMPTZ;                    -- newest row
    "v_source"              TEXT            := 'ads';
    "r"                     RECORD;
BEGIN
    -- count rows
    SELECT COUNT(*)
      INTO "v_count"
      FROM "site_orcamentos"
     WHERE "created_at" > "p_since";
    IF TG_OP = 'INSERT' THEN
        NEW."created_at" := NOW();
    ELSIF TG_OP = 'UPDATE' THEN
        NULL;
    ELSE
        RAISE EXCEPTION 'bad op %', TG_OP;
    END IF;
    FOR "i" IN 1..10 LOOP
        EXIT WHEN "i" > "v_count";
    END LOOP;
    FOR "r" IN
        SELECT "id"
          FROM "site_orcamentos"
    LOOP
        PERFORM PG_NOTIFY('x', "r"."id"::TEXT);
    END LOOP;
    BEGIN
        INSERT INTO "t" (
            "a"
        )
        VALUES (1)
        RETURNING "id"
          INTO "v_count";
    EXCEPTION
        WHEN unique_violation THEN
            "v_count" := 0;
    END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
