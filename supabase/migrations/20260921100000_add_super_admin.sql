begin;

set local search_path = public;

alter table public.users drop constraint if exists users_role_check;
alter table public.users add constraint users_role_check
    check (role in ('member','manager','ketua_i','ketua_ii','ketua_utama','super_admin'));
create unique index idx_users_one_active_super_admin on public.users(role)
    where role = 'super_admin' and active = true and historical_identity = false;

create table public.super_admin_overrides (
    id text primary key,
    request_type text not null check (request_type in ('loan','withdrawal')),
    request_id text not null,
    actor_id text not null references public.users(id),
    previous_stage text not null default '',
    decision text not null check (decision in ('approved','rejected')),
    reason text not null default '',
    created_at timestamp not null default current_timestamp,
    unique (request_type, request_id)
);

create index idx_super_admin_overrides_request
    on public.super_admin_overrides(request_type, request_id, created_at);

create table public.admin_audit_events (
    id text primary key,
    actor_id text not null references public.users(id),
    method text not null,
    path text not null,
    status integer not null,
    request_id text not null default '',
    created_at timestamp not null default current_timestamp
);

create index idx_admin_audit_events_actor_created
    on public.admin_audit_events(actor_id, created_at, id);

alter table public.super_admin_overrides enable row level security;
alter table public.admin_audit_events enable row level security;

create or replace function public.protect_super_admin_audit_tables()
returns trigger language plpgsql set search_path = pg_catalog as $$
begin
    if tg_op in ('UPDATE', 'DELETE') then
        raise exception 'super admin audit records are append-only';
    end if;
    if not exists (
        select 1 from public.users
        where id = new.actor_id
          and role = 'super_admin'
          and active = true
          and historical_identity = false
    ) then
        raise exception 'super admin actor is required';
    end if;
    return new;
end
$$;

create trigger super_admin_overrides_append_only
before insert or update or delete on public.super_admin_overrides
for each row execute function public.protect_super_admin_audit_tables();

create trigger admin_audit_events_append_only
before insert or update or delete on public.admin_audit_events
for each row execute function public.protect_super_admin_audit_tables();

drop trigger if exists protect_proposed_loan_terms_identity on public.loan_requests;
drop function if exists public.protect_proposed_loan_terms_identity();

create function public.protect_proposed_loan_terms_identity()
returns trigger language plpgsql set search_path = pg_catalog as $$
begin
    if old.proposed_admin_fee_policy is not null
       and (old.loan_type is distinct from new.loan_type
            or old.legacy_terms is distinct from new.legacy_terms) then
        raise exception 'loan terms identity is immutable after snapshot';
    end if;
    if old.proposed_admin_fee_policy is null
       and new.proposed_admin_fee_policy is not null
       and (old.status <> 'pending' or old.current_approval_stage <> 'manager')
       and not exists (
           select 1 from public.super_admin_overrides o
           where o.request_type = 'loan'
             and o.request_id = old.id
             and o.decision = 'approved'
       ) then
        raise exception 'proposed loan admin fee snapshot must be assigned at Manager stage';
    end if;
    return new;
end
$$;

create trigger protect_proposed_loan_terms_identity
before update of loan_type, legacy_terms, proposed_admin_fee_policy on public.loan_requests
for each row execute function public.protect_proposed_loan_terms_identity();

drop trigger if exists validate_loan_request_state_integrity on public.loan_requests;
drop function if exists public.validate_loan_request_state_integrity();

create function public.validate_loan_request_state_integrity()
returns trigger language plpgsql as $$
declare
    approved_current boolean;
    approved_manager boolean;
    override_decision boolean;
begin
    if old.legacy_terms = false then
        if new.legacy_terms <> false then
            raise exception 'legacy loan terms are migration-only';
        end if;
        if new.status = 'pending'
           and (new.current_approval_stage is null
                or new.current_approval_stage not in ('manager','ketua_i','ketua_ii','ketua_utama')
                or (new.current_approval_stage <> 'manager' and new.proposed_admin_fee_policy is null)) then
            raise exception 'invalid loan request approval state';
        end if;
        if new.status = 'approved'
           and (new.current_approval_stage is not null or new.proposed_admin_fee_policy is null) then
            raise exception 'invalid approved loan request state';
        end if;
        if new.status in ('rejected','cancelled') and new.current_approval_stage is not null then
            raise exception 'invalid terminal loan request state';
        end if;

        select exists (
            select 1 from public.loan_request_approvals a
            where a.request_id = old.id
              and a.stage = old.current_approval_stage
              and a.decision = 'approved'
        ) into approved_current;
        select exists (
            select 1 from public.super_admin_overrides o
            where o.request_type = 'loan'
              and o.request_id = old.id
              and o.decision = new.status
        ) into override_decision;

        if not (
            (old.status = 'pending' and new.status = 'pending'
                and old.current_approval_stage is not distinct from new.current_approval_stage)
            or (old.status = 'pending' and new.status = 'pending'
                and ((old.current_approval_stage = 'manager' and new.current_approval_stage = 'ketua_ii')
                    or (old.current_approval_stage = 'ketua_ii' and new.current_approval_stage = 'ketua_i')
                    or (old.current_approval_stage = 'ketua_i' and new.current_approval_stage = 'ketua_utama'))
                and approved_current)
            or (old.status = 'pending' and new.status in ('approved','rejected') and override_decision)
            or (old.status = 'pending' and new.status = 'approved'
                and old.current_approval_stage = 'ketua_utama' and approved_current)
            or (old.status = 'pending' and new.status = 'rejected'
                and exists (
                    select 1 from public.loan_request_approvals a
                    where a.request_id = old.id
                      and a.stage = old.current_approval_stage
                      and a.decision = 'rejected'
                ))
            or (old.status = 'pending' and new.status = 'cancelled')
            or (old.status <> 'pending' and old.status = new.status
                and old.current_approval_stage is not distinct from new.current_approval_stage)
        ) then
            raise exception 'invalid loan request state transition';
        end if;

        select exists (
            select 1 from public.loan_request_approvals a
            where a.request_id = new.id
              and a.stage = 'manager'
              and a.decision = 'approved'
        ) into approved_manager;
        if new.proposed_admin_fee_policy is not null
           and (new.current_approval_stage <> 'manager' or new.status <> 'pending')
           and not approved_manager
           and not exists (
               select 1 from public.super_admin_overrides o
               where o.request_type = 'loan'
                 and o.request_id = new.id
                 and o.decision = 'approved'
           ) then
            raise exception 'Manager approval is required for snapshotted terms';
        end if;
    end if;
    return new;
end
$$;

create trigger validate_loan_request_state_integrity
before update of status, current_approval_stage, legacy_terms,
    proposed_admin_fee_policy, proposed_monthly_admin_fee,
    proposed_total_admin_fee, proposed_total_obligation,
    proposed_approved_amount, proposed_duration_months, proposed_start_date
on public.loan_requests for each row execute function public.validate_loan_request_state_integrity();

drop trigger if exists validate_loan_request_provenance on public.loans;
drop function if exists public.validate_loan_request_provenance();

create function public.validate_loan_request_provenance()
returns trigger language plpgsql as $$
declare
    override_approved boolean;
begin
    select exists (
        select 1 from public.super_admin_overrides o
        where o.request_type = 'loan'
          and o.request_id = new.loan_request_id
          and o.decision = 'approved'
    ) into override_approved;

    if new.legacy_terms = false
       and not override_approved
       and (
           not exists (
               select 1 from public.loan_requests r
               where r.id = new.loan_request_id
                 and r.status = 'approved'
                 and r.current_approval_stage is null
                 and r.legacy_terms = false
                 and r.member_id = new.member_id
                 and r.loan_type = new.loan_type
                 and r.proposed_approved_amount = new.approved_amount
                 and r.proposed_duration_months = new.duration_months
                 and r.proposed_start_date = new.start_date
                 and r.proposed_admin_fee_policy = new.admin_fee_policy
                 and r.proposed_monthly_admin_fee is not distinct from new.monthly_admin_fee
                 and r.proposed_total_admin_fee = new.total_admin_fee
                 and r.proposed_total_obligation = new.total_obligation
           )
           or new.remaining_balance <> new.total_obligation
           or (select count(*) from public.loan_request_approvals a
               where a.request_id = new.loan_request_id
                 and a.decision = 'approved'
                 and a.stage in ('manager','ketua_i','ketua_ii','ketua_utama')) <> 4
           or not exists (
               select 1 from public.loan_request_approvals a
               where a.request_id = new.loan_request_id
                 and a.stage = 'ketua_utama'
                 and a.decision = 'approved'
                 and a.officer_id = new.approved_by
           )
       ) then
        raise exception 'loan must match a fully approved loan request';
    end if;
    return new;
end
$$;

create trigger validate_loan_request_provenance
before insert on public.loans
for each row execute function public.validate_loan_request_provenance();

insert into public.schema_migrations (version, name)
values (28, 'add_super_admin_support')
on conflict (version) do nothing;

commit;
