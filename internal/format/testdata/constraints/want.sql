CREATE TABLE "orders" (
    "id"                BIGSERIAL           PRIMARY KEY,
    "customer_id"       BIGINT                              NOT NULL                        REFERENCES "customers" ("id") ON DELETE CASCADE,
    "amount"            NUMERIC(10, 2)                      NOT NULL    DEFAULT 0           CHECK ("amount" >= 0),
    "status"            "order_status"                      NOT NULL,
    "code"              TEXT                UNIQUE,
    "created_at"        TIMESTAMPTZ                         NOT NULL    DEFAULT NOW(),

    CONSTRAINT      "orders_amount_check"       CHECK ("amount" < 1000000),
                                                UNIQUE ("customer_id", "created_at"),
                                                FOREIGN KEY ("customer_id") REFERENCES "customers" ("id") MATCH FULL ON UPDATE RESTRICT
);
