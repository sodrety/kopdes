alter table public.members
    add column bank_name text not null default '';

alter table public.members
    add column bank_account text not null default '';

alter table public.loan_requests
    drop constraint if exists loan_requests_requested_amount_max;

alter table public.loan_requests
    drop constraint if exists loan_requests_proposed_approved_amount_max;

alter table public.loans
    drop constraint if exists loans_approved_amount_max;

create or replace function public.validate_loan_request_amount_against_savings()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
declare
    saving_balance numeric;
begin
    select coalesce(sum(case when type = 'deposit' then amount::numeric else -amount::numeric end), 0)
    into saving_balance
    from public.saving_records
    where member_id = new.member_id;

    if new.requested_amount::numeric > saving_balance * 4 then
        raise exception 'loan amount exceeds four times saving balance';
    end if;
    return new;
end;
$$;

drop trigger if exists loan_requests_requested_amount_savings_limit on public.loan_requests;

create trigger loan_requests_requested_amount_savings_limit
before insert or update of requested_amount, member_id on public.loan_requests
for each row execute function public.validate_loan_request_amount_against_savings();

create or replace function public.validate_loan_proposed_amount_against_savings()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
declare
    saving_balance numeric;
begin
    select coalesce(sum(case when type = 'deposit' then amount::numeric else -amount::numeric end), 0)
    into saving_balance
    from public.saving_records
    where member_id = new.member_id;

    if new.proposed_approved_amount is not null and new.proposed_approved_amount::numeric > saving_balance * 4 then
        raise exception 'approved loan amount exceeds four times saving balance';
    end if;
    return new;
end;
$$;

drop trigger if exists loan_requests_proposed_amount_savings_limit on public.loan_requests;

create trigger loan_requests_proposed_amount_savings_limit
before insert or update of proposed_approved_amount, member_id on public.loan_requests
for each row execute function public.validate_loan_proposed_amount_against_savings();

create or replace function public.validate_loan_approved_amount_against_savings()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
declare
    saving_balance numeric;
begin
    select coalesce(sum(case when type = 'deposit' then amount::numeric else -amount::numeric end), 0)
    into saving_balance
    from public.saving_records
    where member_id = new.member_id;

    if new.approved_amount::numeric > saving_balance * 4 then
        raise exception 'approved loan amount exceeds four times saving balance';
    end if;
    return new;
end;
$$;

drop trigger if exists loans_approved_amount_savings_limit on public.loans;

create trigger loans_approved_amount_savings_limit
before insert or update of approved_amount, member_id on public.loans
for each row execute function public.validate_loan_approved_amount_against_savings();
