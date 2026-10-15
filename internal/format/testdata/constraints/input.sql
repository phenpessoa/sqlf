CREATE TABLE orders (
  id bigserial primary key,
  customer_id bigint not null references customers(id) on delete cascade,
  amount numeric(10,2) not null default 0 check (amount >= 0),
  status order_status not null,
  code text unique,
  created_at timestamp with time zone not null default now(),
  constraint orders_amount_check check (amount < 1000000),
  unique (customer_id, created_at),
  foreign key (customer_id) references customers (id) match full on update restrict
);
