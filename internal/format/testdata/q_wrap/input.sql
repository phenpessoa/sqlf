select first_name || ' ' || middle_name || ' ' || last_name || ' <' || email || '>' || ' (' || company_name || ')' as display from contacts;
select 1 from o where o.status in ('new', 'pending', 'review', 'approved', 'shipped', 'delivered', 'returned', 'cancelled', 'refunded', 'lost');
