alter table whatsapp_tracking
add column if not exists gclid text not null default '',
add utm_campaign_id bigint,
drop column if exists keyword,
drop column legacy_source cascade,
alter column page set not null,
alter column page type varchar(255) using page::varchar,
alter column hash set default md5(''),
alter column hash drop default,
add constraint wt_hash_key unique (hash),
drop constraint if exists old_key;

alter table only site_orcamentos enable row level security;
