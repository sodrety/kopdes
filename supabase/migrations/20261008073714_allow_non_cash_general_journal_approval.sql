begin;
set local statement_timeout = '30s';

alter table public.manual_cash_transaction_drafts
    drop constraint if exists manual_cash_transaction_drafts_check;

alter table public.manual_cash_transaction_drafts
    add constraint manual_cash_transaction_drafts_approval_state_check check (
        (status='pending' and approved_by is null and transaction_id is null and approved_at is null)
        or
        (status='approved' and approved_by is not null and approved_at is not null
            and (entry_type='journal' or transaction_id is not null))
    );

insert into public.schema_migrations (version, name)
values (39, 'allow_non_cash_general_journal_approval')
on conflict (version) do nothing;

commit;
