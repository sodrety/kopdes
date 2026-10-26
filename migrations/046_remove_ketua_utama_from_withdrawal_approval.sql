BEGIN;
SET LOCAL statement_timeout = '30s';

WITH eligible AS (
    SELECT wr.id
    FROM withdrawal_requests wr
    JOIN members m ON m.id=wr.member_id AND m.status='active'
    JOIN withdrawal_reservations r ON r.request_id=wr.id AND r.member_id=wr.member_id AND r.amount=wr.amount AND r.status='active'
    JOIN withdrawal_request_approvals manager ON manager.request_id=wr.id AND manager.stage='manager' AND manager.decision='approved'
    JOIN withdrawal_request_approvals ketua_ii ON ketua_ii.request_id=wr.id AND ketua_ii.stage='ketua_ii' AND ketua_ii.decision='approved'
    JOIN withdrawal_request_approvals ketua_i ON ketua_i.request_id=wr.id AND ketua_i.stage='ketua_i' AND ketua_i.decision='approved'
    WHERE wr.status='pending' AND wr.current_approval_stage='ketua_utama'
      AND wr.disbursement_date='' AND wr.disbursed_by IS NULL AND wr.disbursed_at IS NULL AND wr.saving_record_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=wr.id AND a.stage='ketua_utama')
)
INSERT INTO notification_events (id,event_type,request_type,request_id,payload)
SELECT 'migration-046-withdrawal-approved-event-' || id,'request_approved','withdrawal',id,'{"outcome":"approved"}'
FROM eligible WHERE TRUE
ON CONFLICT (id) DO NOTHING;

WITH eligible AS (
    SELECT wr.id
    FROM withdrawal_requests wr
    JOIN members m ON m.id=wr.member_id AND m.status='active'
    JOIN withdrawal_reservations r ON r.request_id=wr.id AND r.member_id=wr.member_id AND r.amount=wr.amount AND r.status='active'
    JOIN withdrawal_request_approvals manager ON manager.request_id=wr.id AND manager.stage='manager' AND manager.decision='approved'
    JOIN withdrawal_request_approvals ketua_ii ON ketua_ii.request_id=wr.id AND ketua_ii.stage='ketua_ii' AND ketua_ii.decision='approved'
    JOIN withdrawal_request_approvals ketua_i ON ketua_i.request_id=wr.id AND ketua_i.stage='ketua_i' AND ketua_i.decision='approved'
    WHERE wr.status='pending' AND wr.current_approval_stage='ketua_utama'
      AND wr.disbursement_date='' AND wr.disbursed_by IS NULL AND wr.disbursed_at IS NULL AND wr.saving_record_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=wr.id AND a.stage='ketua_utama')
)
INSERT INTO notification_events (id,event_type,request_type,request_id,payload)
SELECT 'migration-046-withdrawal-ready-event-' || id,'withdrawal_disbursement_ready','withdrawal',id,'{}'
FROM eligible WHERE TRUE
ON CONFLICT (id) DO NOTHING;

WITH eligible AS (
    SELECT wr.id
    FROM withdrawal_requests wr
    JOIN members m ON m.id=wr.member_id AND m.status='active'
    JOIN withdrawal_reservations r ON r.request_id=wr.id AND r.member_id=wr.member_id AND r.amount=wr.amount AND r.status='active'
    JOIN withdrawal_request_approvals manager ON manager.request_id=wr.id AND manager.stage='manager' AND manager.decision='approved'
    JOIN withdrawal_request_approvals ketua_ii ON ketua_ii.request_id=wr.id AND ketua_ii.stage='ketua_ii' AND ketua_ii.decision='approved'
    JOIN withdrawal_request_approvals ketua_i ON ketua_i.request_id=wr.id AND ketua_i.stage='ketua_i' AND ketua_i.decision='approved'
    WHERE wr.status='pending' AND wr.current_approval_stage='ketua_utama'
      AND wr.disbursement_date='' AND wr.disbursed_by IS NULL AND wr.disbursed_at IS NULL AND wr.saving_record_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=wr.id AND a.stage='ketua_utama')
)
UPDATE notifications
SET resolved_at=CURRENT_TIMESTAMP
WHERE resolved_at IS NULL
  AND event_id IN (
      SELECT e.id FROM notification_events e JOIN eligible ON eligible.id=e.request_id
      WHERE e.request_type='withdrawal'
  );

WITH eligible AS (
    SELECT wr.id,ketua_i.officer_id,ketua_i.created_at
    FROM withdrawal_requests wr
    JOIN members m ON m.id=wr.member_id AND m.status='active'
    JOIN withdrawal_reservations r ON r.request_id=wr.id AND r.member_id=wr.member_id AND r.amount=wr.amount AND r.status='active'
    JOIN withdrawal_request_approvals manager ON manager.request_id=wr.id AND manager.stage='manager' AND manager.decision='approved'
    JOIN withdrawal_request_approvals ketua_ii ON ketua_ii.request_id=wr.id AND ketua_ii.stage='ketua_ii' AND ketua_ii.decision='approved'
    JOIN withdrawal_request_approvals ketua_i ON ketua_i.request_id=wr.id AND ketua_i.stage='ketua_i' AND ketua_i.decision='approved'
    WHERE wr.status='pending' AND wr.current_approval_stage='ketua_utama'
      AND wr.disbursement_date='' AND wr.disbursed_by IS NULL AND wr.disbursed_at IS NULL AND wr.saving_record_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=wr.id AND a.stage='ketua_utama')
)
UPDATE withdrawal_requests
SET status='approved',
    current_approval_stage=NULL,
    reviewed_by=(SELECT eligible.officer_id FROM eligible WHERE eligible.id=withdrawal_requests.id),
    reviewed_at=(SELECT eligible.created_at FROM eligible WHERE eligible.id=withdrawal_requests.id),
    updated_at=CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM eligible);

INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link,audience)
SELECT 'migration-046-withdrawal-approved-notification-' || wr.id || '-' || u.id,
       e.id,u.id,'notification_approved_title','notification_approved_body','/member/withdrawal-requests','member'
FROM withdrawal_requests wr
JOIN notification_events e ON e.id='migration-046-withdrawal-approved-event-' || wr.id
JOIN users u ON u.member_id=wr.member_id AND u.historical_identity=FALSE AND u.active=TRUE
WHERE wr.status='approved' AND wr.current_approval_stage IS NULL
ON CONFLICT (id) DO NOTHING;

INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link,audience)
SELECT 'migration-046-withdrawal-ready-notification-' || wr.id || '-' || u.id,
       e.id,u.id,'notification_withdrawal_disbursement_ready_title','notification_withdrawal_disbursement_ready_body','/admin/withdrawal-disbursements','officer'
FROM withdrawal_requests wr
JOIN notification_events e ON e.id='migration-046-withdrawal-ready-event-' || wr.id
JOIN members m ON m.id=wr.member_id AND m.status='active'
JOIN officer_appointments oa ON oa.member_id=m.id AND oa.role='bendahara' AND oa.active=TRUE
JOIN users u ON u.member_id=m.id AND u.historical_identity=FALSE AND u.active=TRUE
WHERE wr.status='approved' AND wr.current_approval_stage IS NULL
ON CONFLICT (id) DO NOTHING;

COMMIT;
