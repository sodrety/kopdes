begin;

create temporary table migration_27_remapped_loan_requests on commit drop as
select id
from public.loan_requests
where status = 'pending'
  and current_approval_stage = 'ketua_i'
  and not exists (
      select 1
      from public.loan_request_approvals a
      where a.request_id = public.loan_requests.id
        and a.stage = 'ketua_ii'
        and a.decision = 'approved'
  );

drop trigger if exists validate_loan_request_state_integrity on public.loan_requests;
drop function if exists public.validate_loan_request_state_integrity();

update public.loan_requests
set current_approval_stage = 'ketua_ii', updated_at = current_timestamp
where id in (select id from migration_27_remapped_loan_requests);

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
            or (old.status = 'pending' and new.status = 'pending' and ((old.current_approval_stage = 'manager' and new.current_approval_stage = 'ketua_ii') or (old.current_approval_stage = 'ketua_ii' and new.current_approval_stage = 'ketua_i') or (old.current_approval_stage = 'ketua_i' and new.current_approval_stage = 'ketua_utama')) and approved_current)
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

create trigger validate_loan_request_state_integrity
before update of status, current_approval_stage, legacy_terms, proposed_admin_fee_policy, proposed_monthly_admin_fee, proposed_total_admin_fee, proposed_total_obligation, proposed_approved_amount, proposed_duration_months, proposed_start_date
on public.loan_requests for each row execute function public.validate_loan_request_state_integrity();

update public.notifications n
set resolved_at = current_timestamp
from public.notification_events e
where n.event_id = e.id
  and n.resolved_at is null
  and e.request_type = 'loan'
  and e.request_id in (select id from migration_27_remapped_loan_requests);

insert into public.notification_events (id, event_type, request_type, request_id, payload)
select 'migration-27-loan-stage-event-' || id,
       'approval_stage_ready',
       'loan',
       id,
       '{"stage":"ketua_ii"}'
from migration_27_remapped_loan_requests;

insert into public.notifications (id, event_id, user_id, title_key, body_key, link, audience)
select 'migration-27-loan-stage-notification-' || r.id || '-' || u.id,
       'migration-27-loan-stage-event-' || r.id,
       u.id,
       'notification_approval_title',
       'notification_approval_body',
       '/admin/loan-requests',
       'officer'
from migration_27_remapped_loan_requests r
join public.officer_appointments oa on oa.role = 'ketua_ii' and oa.active = true
join public.members m on m.id = oa.member_id and m.status = 'active'
join public.users u on u.member_id = m.id and u.historical_identity = false and u.active = true;

insert into public.schema_migrations (version, name)
values (27, 'change_loan_approval_hierarchy')
on conflict (version) do nothing;

commit;
