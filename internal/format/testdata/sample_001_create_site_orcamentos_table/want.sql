CREATE TABLE IF NOT EXISTS "site_orcamentos" (
    "id"            BIGSERIAL       PRIMARY KEY     NOT NULL,
    "name"          TEXT                            NOT NULL,
    "email"         TEXT                            NOT NULL,
    "phone"         TEXT                            NOT NULL,
    "message"       TEXT                            NOT NULL,
    "created_at"    TIMESTAMPTZ                     NOT NULL    DEFAULT NOW()
);

---- create above / drop below ----

DROP TABLE IF EXISTS "site_orcamentos";
