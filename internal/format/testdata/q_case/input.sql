select id, case when source = 'ads' then 'paid' when source = 'organic' then 'free' else 'other' end as channel from whatsapp_tracking;
