create unique index if not exists site_orcamentos_email_idx on site_orcamentos (email) where deleted_at is null;
create index wt_created_at_idx on whatsapp_tracking using brin (created_at);
create index concurrently on t (lower(email), (a + b) desc nulls last) include (c);
