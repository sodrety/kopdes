begin;
set local statement_timeout = '30s';

create table public.loan_repayment_audits (
    id text primary key,
    repayment_id text not null,
    loan_id text not null references public.loans(id),
    member_id text not null references public.members(id),
    action text not null check (action in ('edited','removed')),
    reason text not null,
    before_state text not null,
    after_state text not null default '',
    actor_id text not null references public.users(id),
    created_at timestamp not null default current_timestamp
);

create index idx_loan_repayment_audits_created
    on public.loan_repayment_audits(created_at,id);
create index idx_loan_repayment_audits_repayment
    on public.loan_repayment_audits(repayment_id,created_at,id);

alter table public.loan_repayment_audits enable row level security;

create or replace function public.protect_loan_repayment_audit_rows()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
begin
    if tg_op in ('UPDATE','DELETE') then
        raise exception 'loan repayment audit rows are append-only';
    end if;
    if not exists (
        select 1 from public.users
        where id=new.actor_id
          and role='super_admin'
          and active=true
          and historical_identity=false
    ) then
        raise exception 'active super admin actor is required';
    end if;
    return new;
end
$$;

create trigger loan_repayment_audits_append_only
before insert or update or delete on public.loan_repayment_audits
for each row execute function public.protect_loan_repayment_audit_rows();

insert into public.schema_migrations (version,name)
values (40,'audit_super_admin_repayment_corrections')
on conflict (version) do nothing;

commit;
