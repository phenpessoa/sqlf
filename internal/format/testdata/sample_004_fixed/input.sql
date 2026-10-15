CREATE TABLE IF NOT EXISTS whatsapp_tracking (
    "id"            BIGSERIAL   PRIMARY KEY     NOT NULL,
    "href"          TEXT                        NOT NULL,
    "page"          TEXT                        NOT NULL,
    "source"        TEXT                        NOT NULL,
    "hash"          TEXT                        NOT NULL,
    "created_at"    TIMESTAMPTZ                 NOT NULL        DEFAULT NOW()
);

---- create above / drop below ----

DROP TABLE IF EXISTS whatsapp_tracking;
