create trigger t before update of email, name on site_orcamentos
for each row when (old.email is distinct from new.email) execute procedure f('x', 'y');
create trigger t2 after insert or delete on t execute function s.g();
