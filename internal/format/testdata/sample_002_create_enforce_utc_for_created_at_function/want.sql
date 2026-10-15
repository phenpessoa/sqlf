CREATE OR REPLACE FUNCTION "enforce_utc_for_created_at"()
RETURNS TRIGGER AS $$
BEGIN
    NEW."created_at" := NEW."created_at" AT TIME ZONE 'UTC';
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

---- create above / drop below ----

DROP FUNCTION IF EXISTS "enforce_utc_for_created_at"();
