-- +goose Up
-- +goose StatementBegin
create or replace function f() returns trigger as $$
begin
  return new;
end;
$$ language plpgsql;
-- +goose StatementEnd

-- +goose Down
drop function if exists f();
