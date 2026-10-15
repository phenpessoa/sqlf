delete from whatsapp_tracking using site_orcamentos o where whatsapp_tracking.page = o.email;
select id, row_number() over (partition by source order by created_at desc) as rn from whatsapp_tracking limit 10 offset 20;
select email from a union all select email from b;
