-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION "f"()
RETURNS TRIGGER AS $$
BEGIN
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS "f"();
