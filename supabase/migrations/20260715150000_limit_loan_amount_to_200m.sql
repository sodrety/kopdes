drop index if exists public.idx_loans_one_active_per_member;

alter table public.loan_requests
    add constraint loan_requests_requested_amount_max
    check (requested_amount <= 200000000) not valid;

alter table public.loan_requests
    add constraint loan_requests_proposed_approved_amount_max
    check (proposed_approved_amount is null or proposed_approved_amount <= 200000000) not valid;

alter table public.loans
    add constraint loans_approved_amount_max
    check (approved_amount <= 200000000) not valid;
