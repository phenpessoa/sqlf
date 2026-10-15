# sqlf

An opinionated formatter for PostgreSQL `.sql` files. No configs, one style only.

```sh
go install github.com/phenpessoa/sqlf@latest
```

## Examples

### Tables

Before:

```sql
create table if not exists users (
  id bigserial primary key not null,
  email text not null unique,
  name varchar(100),
  created_at timestamp with time zone not null default now()
);
```

After:

```sql
CREATE TABLE IF NOT EXISTS "users" (
    "id"            BIGSERIAL       PRIMARY KEY     NOT NULL,
    "email"         TEXT            UNIQUE          NOT NULL,
    "name"          VARCHAR(100),
    "created_at"    TIMESTAMPTZ                     NOT NULL    DEFAULT NOW()
);
```

### Queries

Before:

```sql
select u.id, u.email, count(o.id) total from users u left join orders o on o.user_id = u.id and o.status != 'canceled' where u.created_at > now() - interval '30 days' group by u.id, u.email order by total desc limit 10;
```

After:

```sql
SELECT "u"."id",
       "u"."email",
       COUNT("o"."id")      AS "total"
  FROM "users"          AS "u"
  LEFT JOIN "orders"    AS "o"
    ON "o"."user_id" = "u"."id"
   AND "o"."status" <> 'canceled'
 WHERE "u"."created_at" > NOW() - '30 days'::INTERVAL
 GROUP BY "u"."id", "u"."email"
 ORDER BY "total" DESC
 LIMIT 10;
```

### Functions

Before:

```sql
create or replace function touch_updated_at() returns trigger as $$
begin
new.updated_at := now();
return new;
end;
$$ language plpgsql;
```

After:

```sql
CREATE OR REPLACE FUNCTION "touch_updated_at"()
RETURNS TRIGGER AS $$
BEGIN
    NEW."updated_at" := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

## Usage

```sh
sqlf [flags] [path ...]
```

| Flag      | What it does                                                |
|-----------|-------------------------------------------------------------|
| (none)    | Print the formatted file to stdout.                         |
| `-w`      | Write the result back to the file.                          |
| `-l`      | List the files that are not formatted.                      |
| `-d`      | Print a diff of what would change.                          |
| `--check` | List the files that are not formatted, and exit 1 if any.   |

- With no path, sqlf reads stdin and writes stdout.
- A directory is searched recursively for `*.sql` files. Directories that start with `.` are skipped.
- Exit code 1 means a file is not formatted (`--check`), does not parse, or could not be read or written.

```sh
sqlf file.sql             # print formatted file
sqlf -w migrations/       # format in place
sqlf -d migrations/       # review changes
sqlf --check migrations/  # CI
```
