begin;

set local search_path = public;

alter table public.officer_appointments drop constraint if exists officer_appointments_role_check;
alter table public.officer_appointments add constraint officer_appointments_role_check
    check (role in ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'));

alter table public.loan_request_approvals drop constraint if exists loan_request_approvals_officer_role_check;
alter table public.loan_request_approvals add constraint loan_request_approvals_officer_role_check
    check (officer_role in ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'));

alter table public.withdrawal_request_approvals drop constraint if exists withdrawal_request_approvals_officer_role_check;
alter table public.withdrawal_request_approvals add constraint withdrawal_request_approvals_officer_role_check
    check (officer_role in ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'));

create table public.member_tagihan_configs (
    member_id text primary key references public.members(id),
    simpanan_wajib bigint not null default 0 check (simpanan_wajib >= 0),
    simpanan_manasuka bigint not null default 0 check (simpanan_manasuka >= 0),
    created_at timestamp not null default current_timestamp,
    updated_at timestamp not null default current_timestamp
);

insert into public.member_tagihan_configs (member_id, simpanan_wajib, simpanan_manasuka)
select m.id,
    coalesce((select case when sr.type='deposit' then sr.amount else 0 end
        from public.saving_records sr
        where sr.member_id=m.id and sr.category='wajib'
        order by sr.record_date desc,sr.created_at desc,sr.id desc limit 1),0),
    coalesce((select case when sr.type='deposit' then sr.amount else 0 end
        from public.saving_records sr
        where sr.member_id=m.id and sr.category='sukarela'
        order by sr.record_date desc,sr.created_at desc,sr.id desc limit 1),0)
from public.members m
where m.id is not null
on conflict (member_id) do nothing;

alter table public.member_tagihan_configs enable row level security;

insert into public.schema_migrations (version, name)
values (29, 'add_admin_role_and_member_tagihan_config');

commit;
