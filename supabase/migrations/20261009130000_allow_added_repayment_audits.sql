begin;
set local statement_timeout = '30s';

alter table public.loan_repayment_audits
    drop constraint if exists loan_repayment_audits_action_check;

alter table public.loan_repayment_audits
    add constraint loan_repayment_audits_action_check
    check (action in ('added','edited','removed'));

insert into public.schema_migrations (version,name)
values (41,'allow_added_repayment_audits')
on conflict (version) do nothing;

commit;
