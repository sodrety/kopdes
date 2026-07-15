begin;
set local statement_timeout = '30s';

alter table public.loan_requests alter column requested_amount type bigint;
alter table public.saving_records alter column amount type bigint;
alter table public.withdrawal_requests alter column amount type bigint;
alter table public.withdrawal_reservations alter column amount type bigint;
alter table public.loan_requests alter column proposed_approved_amount type bigint;
alter table public.loans alter column approved_amount type bigint;
alter table public.loans alter column monthly_installment type bigint;
alter table public.loans alter column remaining_balance type bigint;
alter table public.loans alter column total_interest type bigint;
alter table public.loans alter column total_obligation type bigint;
alter table public.loan_installments alter column scheduled_amount type bigint;
alter table public.loan_installments alter column paid_amount type bigint;
alter table public.loan_repayments alter column amount type bigint;

alter table public.loan_requests add column proposed_admin_fee_policy text null;
alter table public.loan_requests add column proposed_monthly_admin_fee bigint null;
alter table public.loan_requests add column proposed_total_admin_fee bigint null;
alter table public.loan_requests add column proposed_total_obligation bigint null;

alter table public.loans add column admin_fee_policy text null;
alter table public.loans add column monthly_admin_fee bigint null;
alter table public.loans add column total_admin_fee bigint null;

update public.loans
set admin_fee_policy = 'legacy_flat_monthly',
    monthly_admin_fee = null,
    total_admin_fee = total_interest;

alter table public.loans alter column admin_fee_policy set not null;
alter table public.loans alter column total_admin_fee set not null;
alter table public.loans add constraint loans_admin_fee_policy_check check (admin_fee_policy in ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','legacy_flat_monthly'));
alter table public.loans add constraint loans_monthly_admin_fee_check check (monthly_admin_fee is null or monthly_admin_fee >= 0);
alter table public.loans add constraint loans_total_admin_fee_check check (total_admin_fee >= 0);
alter table public.loans add constraint loans_admin_fee_policy_fields_check check (
    (admin_fee_policy = 'regular_tiered_monthly_v1'
      and approved_amount > 0 and duration_months between 1 and 24
      and monthly_admin_fee is not null
      and monthly_admin_fee = case when approved_amount <= 25000000 then (approved_amount + 50) / 100
          else 250000 + ((approved_amount - 25000000) / 200) * 3 + ((((approved_amount - 25000000) % 200) * 3 + 100) / 200) end
      and total_admin_fee is not null and total_admin_fee % nullif(duration_months, 0) = 0 and total_admin_fee / nullif(duration_months, 0) = monthly_admin_fee
      and total_obligation is not null and total_obligation >= approved_amount and total_obligation - approved_amount = total_admin_fee)
    or (admin_fee_policy = 'secondary_goods_one_time_v1'
      and approved_amount > 0 and duration_months between 1 and 12
      and monthly_admin_fee is null
      and total_admin_fee = (approved_amount * 20 + 50) / 100
      and total_obligation is not null and total_obligation - approved_amount = total_admin_fee)
    or (admin_fee_policy = 'goods_purchase_paylater_one_time_v1'
      and approved_amount > 0 and duration_months = 1
      and monthly_admin_fee is null
      and total_admin_fee = (approved_amount * 5 + 50) / 100
      and total_obligation is not null and total_obligation - approved_amount = total_admin_fee)
    or (admin_fee_policy = 'legacy_flat_monthly' and monthly_admin_fee is null and total_admin_fee is not null and total_obligation is not null)
);
alter table public.loans add constraint loans_admin_fee_policy_identity_check check (
    (loan_type='regular' and legacy_terms=false and admin_fee_policy='regular_tiered_monthly_v1') or
    (loan_type='secondary_goods' and legacy_terms=false and admin_fee_policy='secondary_goods_one_time_v1') or
    (loan_type='goods_purchase_paylater' and legacy_terms=false and admin_fee_policy='goods_purchase_paylater_one_time_v1') or
    (loan_type='regular' and legacy_terms=true and admin_fee_policy='legacy_flat_monthly')
);

alter table public.loan_requests add constraint loan_requests_admin_fee_policy_check check (proposed_admin_fee_policy is null or proposed_admin_fee_policy in ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','legacy_flat_monthly'));
alter table public.loan_requests add constraint loan_requests_monthly_admin_fee_check check (proposed_monthly_admin_fee is null or proposed_monthly_admin_fee >= 0);
alter table public.loan_requests add constraint loan_requests_total_admin_fee_check check (proposed_total_admin_fee is null or proposed_total_admin_fee >= 0);
alter table public.loan_requests add constraint loan_requests_total_obligation_check check (proposed_total_obligation is null or proposed_total_obligation > 0);
alter table public.loan_requests add constraint loan_requests_admin_fee_fields_check check (
    (proposed_admin_fee_policy is null and proposed_monthly_admin_fee is null and proposed_total_admin_fee is null and proposed_total_obligation is null)
    or (proposed_admin_fee_policy = 'regular_tiered_monthly_v1'
      and proposed_approved_amount > 0 and proposed_duration_months between 1 and 24
      and proposed_monthly_admin_fee is not null
      and proposed_monthly_admin_fee = case when proposed_approved_amount <= 25000000 then (proposed_approved_amount + 50) / 100
          else 250000 + ((proposed_approved_amount - 25000000) / 200) * 3 + ((((proposed_approved_amount - 25000000) % 200) * 3 + 100) / 200) end
      and proposed_total_admin_fee is not null and proposed_total_admin_fee % nullif(proposed_duration_months, 0) = 0 and proposed_total_admin_fee / nullif(proposed_duration_months, 0) = proposed_monthly_admin_fee
      and proposed_total_obligation is not null and proposed_total_obligation >= proposed_approved_amount and proposed_total_obligation - proposed_approved_amount = proposed_total_admin_fee)
    or (proposed_admin_fee_policy = 'secondary_goods_one_time_v1'
      and proposed_approved_amount > 0 and proposed_duration_months between 1 and 12
      and proposed_monthly_admin_fee is null
      and proposed_total_admin_fee = (proposed_approved_amount * 20 + 50) / 100
      and proposed_total_obligation is not null and proposed_total_obligation - proposed_approved_amount = proposed_total_admin_fee)
    or (proposed_admin_fee_policy = 'goods_purchase_paylater_one_time_v1'
      and proposed_approved_amount > 0 and proposed_duration_months = 1
      and proposed_monthly_admin_fee is null
      and proposed_total_admin_fee = (proposed_approved_amount * 5 + 50) / 100
      and proposed_total_obligation is not null and proposed_total_obligation - proposed_approved_amount = proposed_total_admin_fee)
    or (proposed_admin_fee_policy = 'legacy_flat_monthly' and proposed_approved_amount is not null and proposed_duration_months is not null and proposed_monthly_admin_fee is null and proposed_total_admin_fee is not null and proposed_total_obligation is not null)
);
alter table public.loan_requests add constraint loan_requests_admin_fee_policy_identity_check check (
    proposed_admin_fee_policy is null or
    (loan_type='regular' and legacy_terms=false and proposed_admin_fee_policy='regular_tiered_monthly_v1') or
    (loan_type='secondary_goods' and legacy_terms=false and proposed_admin_fee_policy='secondary_goods_one_time_v1') or
    (loan_type='goods_purchase_paylater' and legacy_terms=false and proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1') or
    (loan_type='regular' and legacy_terms=true and proposed_admin_fee_policy='legacy_flat_monthly')
);

create function public.protect_loan_admin_fee_snapshot() returns trigger language plpgsql set search_path = '' as $$
begin
    if old.admin_fee_policy is distinct from new.admin_fee_policy
       or old.monthly_admin_fee is distinct from new.monthly_admin_fee
       or old.total_admin_fee is distinct from new.total_admin_fee
       or old.total_obligation is distinct from new.total_obligation
       or old.approved_amount is distinct from new.approved_amount
       or old.duration_months is distinct from new.duration_months then
        raise exception 'loan admin fee snapshot is immutable';
    end if;
    return new;
end $$;
create trigger protect_loan_admin_fee_snapshot before update of admin_fee_policy, monthly_admin_fee, total_admin_fee, total_obligation, approved_amount, duration_months on public.loans for each row execute function public.protect_loan_admin_fee_snapshot();

create function public.protect_proposed_loan_admin_fee_snapshot() returns trigger language plpgsql set search_path = '' as $$
begin
    if old.proposed_admin_fee_policy is not null and (
       old.proposed_admin_fee_policy is distinct from new.proposed_admin_fee_policy
       or old.proposed_monthly_admin_fee is distinct from new.proposed_monthly_admin_fee
       or old.proposed_total_admin_fee is distinct from new.proposed_total_admin_fee
       or old.proposed_total_obligation is distinct from new.proposed_total_obligation
       or old.proposed_approved_amount is distinct from new.proposed_approved_amount
       or old.proposed_duration_months is distinct from new.proposed_duration_months) then
        raise exception 'proposed loan admin fee snapshot is immutable';
    end if;
    return new;
end $$;
create trigger protect_proposed_loan_admin_fee_snapshot before update of proposed_admin_fee_policy, proposed_monthly_admin_fee, proposed_total_admin_fee, proposed_total_obligation, proposed_approved_amount, proposed_duration_months on public.loan_requests for each row execute function public.protect_proposed_loan_admin_fee_snapshot();

create function public.protect_loan_terms_identity() returns trigger language plpgsql set search_path = '' as $$
begin
    raise exception 'loan terms identity is immutable after snapshot';
end $$;
create trigger protect_loan_terms_identity before update of loan_type, legacy_terms on public.loans for each row execute function public.protect_loan_terms_identity();

create function public.protect_proposed_loan_terms_identity() returns trigger language plpgsql set search_path = '' as $$
begin
    if old.proposed_admin_fee_policy is not null and
       (old.loan_type is distinct from new.loan_type or old.legacy_terms is distinct from new.legacy_terms) then
        raise exception 'loan terms identity is immutable after snapshot';
    end if;
    if old.proposed_admin_fee_policy is null and new.proposed_admin_fee_policy is not null and
       (old.status <> 'pending' or old.current_approval_stage <> 'manager') then
        raise exception 'proposed loan admin fee snapshot must be assigned at Manager stage';
    end if;
    return new;
end $$;
create trigger protect_proposed_loan_terms_identity before update of loan_type, legacy_terms, proposed_admin_fee_policy on public.loan_requests for each row execute function public.protect_proposed_loan_terms_identity();
create function public.validate_proposed_loan_snapshot_insert() returns trigger language plpgsql set search_path = '' as $$
begin
    if new.proposed_admin_fee_policy is not null and (new.status <> 'pending' or new.current_approval_stage <> 'manager') then
        raise exception 'proposed loan admin fee snapshot must be assigned at Manager stage';
    end if;
    return new;
end $$;
create trigger validate_proposed_loan_snapshot_insert before insert on public.loan_requests for each row execute function public.validate_proposed_loan_snapshot_insert();

create function public.validate_loan_request_origin_insert() returns trigger language plpgsql set search_path = '' as $$
begin
    if new.legacy_terms is distinct from false
        or new.status <> 'pending'
        or new.current_approval_stage <> 'manager'
        or new.proposed_admin_fee_policy is not null
        or new.proposed_monthly_admin_fee is not null
        or new.proposed_total_admin_fee is not null
        or new.proposed_total_obligation is not null
        or new.proposed_approved_amount is not null
        or new.proposed_duration_months is not null
        or new.proposed_start_date <> '' then
        raise exception 'new loan requests must begin nonlegacy at Manager stage';
    end if;
    return new;
end $$;
create trigger validate_loan_request_origin_insert before insert on public.loan_requests for each row execute function public.validate_loan_request_origin_insert();

create function public.protect_loan_request_legacy_provenance() returns trigger language plpgsql set search_path = '' as $$
begin
    if old.legacy_terms = false and new.legacy_terms <> false then
        raise exception 'legacy loan terms are migration-only';
    end if;
    return new;
end $$;
create trigger protect_loan_request_legacy_provenance before update of legacy_terms on public.loan_requests for each row execute function public.protect_loan_request_legacy_provenance();

create function public.protect_loan_legacy_provenance() returns trigger language plpgsql set search_path = '' as $$
begin
    if new.legacy_terms <> false then
        raise exception 'legacy loan terms are migration-only';
    end if;
    return new;
end $$;
create trigger protect_loan_legacy_provenance before insert on public.loans for each row when (new.legacy_terms <> false) execute function public.protect_loan_legacy_provenance();

create function public.validate_loan_request_state_integrity() returns trigger language plpgsql set search_path = '' as $$
declare
    approved_current boolean;
    approved_manager boolean;
begin
    if old.legacy_terms = false then
        if new.legacy_terms <> false then
            raise exception 'legacy loan terms are migration-only';
        end if;
        if new.status = 'pending' and (new.current_approval_stage is null or new.current_approval_stage not in ('manager','ketua_i','ketua_ii','ketua_utama') or (new.current_approval_stage <> 'manager' and new.proposed_admin_fee_policy is null)) then
            raise exception 'invalid loan request approval state';
        end if;
        if new.status = 'approved' and (new.current_approval_stage is not null or new.proposed_admin_fee_policy is null) then
            raise exception 'invalid approved loan request state';
        end if;
        if new.status in ('rejected','cancelled') and new.current_approval_stage is not null then
            raise exception 'invalid terminal loan request state';
        end if;
        select exists (select 1 from public.loan_request_approvals a where a.request_id = old.id and a.stage = old.current_approval_stage and a.decision = 'approved') into approved_current;
        if not (
            (old.status = 'pending' and new.status = 'pending' and old.current_approval_stage is not distinct from new.current_approval_stage)
            or (old.status = 'pending' and new.status = 'pending' and ((old.current_approval_stage = 'manager' and new.current_approval_stage = 'ketua_i') or (old.current_approval_stage = 'ketua_i' and new.current_approval_stage = 'ketua_ii') or (old.current_approval_stage = 'ketua_ii' and new.current_approval_stage = 'ketua_utama')) and approved_current)
            or (old.status = 'pending' and new.status = 'approved' and old.current_approval_stage = 'ketua_utama' and approved_current)
            or (old.status = 'pending' and new.status = 'rejected' and exists (select 1 from public.loan_request_approvals a where a.request_id = old.id and a.stage = old.current_approval_stage and a.decision = 'rejected'))
            or (old.status = 'pending' and new.status = 'cancelled')
            or (old.status <> 'pending' and old.status = new.status and old.current_approval_stage is not distinct from new.current_approval_stage)
        ) then
            raise exception 'invalid loan request state transition';
        end if;
        select exists (select 1 from public.loan_request_approvals a where a.request_id = new.id and a.stage = 'manager' and a.decision = 'approved') into approved_manager;
        if new.proposed_admin_fee_policy is not null and (new.current_approval_stage <> 'manager' or new.status <> 'pending') and not approved_manager then
            raise exception 'Manager approval is required for snapshotted terms';
        end if;
    end if;
    return new;
end $$;
create trigger validate_loan_request_state_integrity before update of status, current_approval_stage, legacy_terms, proposed_admin_fee_policy, proposed_monthly_admin_fee, proposed_total_admin_fee, proposed_total_obligation, proposed_approved_amount, proposed_duration_months, proposed_start_date on public.loan_requests for each row execute function public.validate_loan_request_state_integrity();

create function public.validate_loan_request_provenance() returns trigger language plpgsql set search_path = '' as $$
begin
    if new.legacy_terms = false and (
        not exists (select 1 from public.loan_requests r where r.id = new.loan_request_id and r.status = 'approved' and r.current_approval_stage is null and r.legacy_terms = false and r.member_id = new.member_id and r.loan_type = new.loan_type and r.proposed_approved_amount = new.approved_amount and r.proposed_duration_months = new.duration_months and r.proposed_start_date = new.start_date and r.proposed_admin_fee_policy = new.admin_fee_policy and r.proposed_monthly_admin_fee is not distinct from new.monthly_admin_fee and r.proposed_total_admin_fee = new.total_admin_fee and r.proposed_total_obligation = new.total_obligation)
        or new.remaining_balance <> new.total_obligation
        or (select count(*) from public.loan_request_approvals a where a.request_id = new.loan_request_id and a.decision = 'approved' and a.stage in ('manager','ketua_i','ketua_ii','ketua_utama')) <> 4
        or not exists (select 1 from public.loan_request_approvals a where a.request_id = new.loan_request_id and a.stage = 'ketua_utama' and a.decision = 'approved' and a.officer_id = new.approved_by)
    ) then
        raise exception 'loan must match a fully approved loan request';
    end if;
    return new;
end $$;
create trigger validate_loan_request_provenance before insert on public.loans for each row execute function public.validate_loan_request_provenance();

create schema if not exists private;
revoke all on schema private from anon, authenticated;
create table private.monetary_aggregate_totals (
    singleton boolean primary key default true check (singleton),
    saving_records_total bigint not null check (saving_records_total >= 0),
    withdrawal_requests_total bigint not null check (withdrawal_requests_total >= 0),
    withdrawal_reservations_total bigint not null check (withdrawal_reservations_total >= 0),
    loan_requests_requested_total bigint not null check (loan_requests_requested_total >= 0),
    loans_approved_total bigint not null check (loans_approved_total >= 0),
    loans_obligation_total bigint not null check (loans_obligation_total >= 0),
    loans_remaining_total bigint not null check (loans_remaining_total >= 0),
    loan_repayments_total bigint not null check (loan_repayments_total >= 0),
    loan_installments_scheduled_total bigint not null check (loan_installments_scheduled_total >= 0),
    loan_installments_paid_total bigint not null check (loan_installments_paid_total >= 0)
);
insert into private.monetary_aggregate_totals select true,
       (select coalesce(sum(amount::numeric), 0) from public.saving_records),
       (select coalesce(sum(amount::numeric), 0) from public.withdrawal_requests),
       (select coalesce(sum(amount::numeric), 0) from public.withdrawal_reservations),
       (select coalesce(sum(requested_amount::numeric), 0) from public.loan_requests),
       (select coalesce(sum(approved_amount::numeric), 0) from public.loans),
       (select coalesce(sum(total_obligation::numeric), 0) from public.loans),
       (select coalesce(sum(remaining_balance::numeric), 0) from public.loans),
       (select coalesce(sum(amount::numeric), 0) from public.loan_repayments),
       (select coalesce(sum(scheduled_amount::numeric), 0) from public.loan_installments),
       (select coalesce(sum(paid_amount::numeric), 0) from public.loan_installments);
revoke all on table private.monetary_aggregate_totals from anon, authenticated;

create function private.maintain_monetary_aggregate_total() returns trigger language plpgsql security definer set search_path = pg_catalog as $$
declare
    aggregate_column text := tg_argv[0];
    value_column text := tg_argv[1];
    ledger_schema text := tg_argv[2];
    old_value numeric;
    new_value numeric;
    delta numeric;
    changed bigint;
begin
    if aggregate_column not in ('saving_records_total', 'withdrawal_requests_total', 'withdrawal_reservations_total', 'loan_requests_requested_total', 'loans_approved_total', 'loans_obligation_total', 'loans_remaining_total', 'loan_repayments_total', 'loan_installments_scheduled_total', 'loan_installments_paid_total') then
        raise exception 'invalid monetary aggregate column';
    end if;
    if ledger_schema = '' then ledger_schema := tg_table_schema; end if;
    if ledger_schema not in ('private', tg_table_schema) then
        raise exception 'invalid monetary aggregate schema';
    end if;
    if tg_op <> 'INSERT' then old_value := (to_jsonb(old)->>value_column)::numeric; end if;
    if tg_op <> 'DELETE' then new_value := (to_jsonb(new)->>value_column)::numeric; end if;
    if tg_op = 'INSERT' then delta := new_value;
    elsif tg_op = 'DELETE' then delta := -old_value;
    else delta := new_value - old_value;
    end if;
    execute format('update %I.monetary_aggregate_totals set %I=%I+$1 where singleton=true and (($1>=0 and %I<=9223372036854775807-$1) or ($1<0 and %I>=-$1))', ledger_schema, aggregate_column, aggregate_column, aggregate_column, aggregate_column) using delta;
    get diagnostics changed = row_count;
    if changed <> 1 then
        raise exception using errcode = '22003', message = 'monetary aggregate capacity exceeded';
    end if;
    if tg_op = 'DELETE' then return old; end if;
    return new;
end $$;
revoke all on function private.maintain_monetary_aggregate_total() from public, anon, authenticated;
create trigger saving_records_aggregate_total before insert or delete or update of amount on public.saving_records for each row execute function private.maintain_monetary_aggregate_total('saving_records_total', 'amount', 'private');
create trigger withdrawal_requests_aggregate_total before insert or delete or update of amount on public.withdrawal_requests for each row execute function private.maintain_monetary_aggregate_total('withdrawal_requests_total', 'amount', 'private');
create trigger withdrawal_reservations_aggregate_total before insert or delete or update of amount on public.withdrawal_reservations for each row execute function private.maintain_monetary_aggregate_total('withdrawal_reservations_total', 'amount', 'private');
create trigger loan_requests_requested_aggregate_total before insert or delete or update of requested_amount on public.loan_requests for each row execute function private.maintain_monetary_aggregate_total('loan_requests_requested_total', 'requested_amount', 'private');
create trigger loans_approved_aggregate_total before insert or delete or update of approved_amount on public.loans for each row execute function private.maintain_monetary_aggregate_total('loans_approved_total', 'approved_amount', 'private');
create trigger loans_obligation_aggregate_total before insert or delete or update of total_obligation on public.loans for each row execute function private.maintain_monetary_aggregate_total('loans_obligation_total', 'total_obligation', 'private');
create trigger loans_remaining_aggregate_total before insert or delete or update of remaining_balance on public.loans for each row execute function private.maintain_monetary_aggregate_total('loans_remaining_total', 'remaining_balance', 'private');
create trigger loan_repayments_aggregate_total before insert or delete or update of amount on public.loan_repayments for each row execute function private.maintain_monetary_aggregate_total('loan_repayments_total', 'amount', 'private');
create trigger loan_installments_scheduled_aggregate_total before insert or delete or update of scheduled_amount on public.loan_installments for each row execute function private.maintain_monetary_aggregate_total('loan_installments_scheduled_total', 'scheduled_amount', 'private');
create trigger loan_installments_paid_aggregate_total before insert or delete or update of paid_amount on public.loan_installments for each row execute function private.maintain_monetary_aggregate_total('loan_installments_paid_total', 'paid_amount', 'private');

insert into public.schema_migrations (version, name)
values (16, 'add_regular_loan_admin_fee_terms')
on conflict (version) do nothing;

commit;
