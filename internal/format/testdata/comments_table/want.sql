CREATE TABLE "t" (
    -- identity
    "id"        BIGSERIAL       PRIMARY KEY     NOT NULL,       -- surrogate key
    "email"     TEXT                            NOT NULL,       -- login
    "note"      TEXT
    -- end of columns
);

ALTER TABLE "t"
ADD COLUMN      "a"     TEXT,                                   -- first
-- before b
ADD COLUMN      "bb"    INTEGER     NOT NULL    DEFAULT 0;
