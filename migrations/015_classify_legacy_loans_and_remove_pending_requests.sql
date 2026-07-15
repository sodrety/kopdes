-- PostgreSQL mirror of runtime migration 15 in internal/app/migrations.go.
BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE loan_requests
    ADD COLUMN loan_type TEXT
    CHECK (loan_type IN ('regular', 'secondary_goods', 'goods_purchase_paylater'));
ALTER TABLE loan_requests
    ADD COLUMN legacy_terms BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE loans
    ADD COLUMN loan_type TEXT
    CHECK (loan_type IN ('regular', 'secondary_goods', 'goods_purchase_paylater'));
ALTER TABLE loans
    ADD COLUMN legacy_terms BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE loan_requests
SET loan_type = 'regular', legacy_terms = TRUE
WHERE status <> 'pending';

UPDATE loans
SET loan_type = 'regular', legacy_terms = TRUE;

CREATE TEMPORARY TABLE migration_15_pending_loan_members ON COMMIT DROP AS
SELECT DISTINCT member_id
FROM loan_requests
WHERE status = 'pending';

CREATE TEMPORARY TABLE migration_15_notification_preflight (
    missing_member_count INTEGER NOT NULL CHECK (missing_member_count = 0)
) ON COMMIT DROP;

INSERT INTO migration_15_notification_preflight (missing_member_count)
SELECT COUNT(*)
FROM migration_15_pending_loan_members pending
WHERE (
    SELECT COUNT(*)
    FROM users
    WHERE users.member_id = pending.member_id
      AND users.historical_identity = FALSE
) <> 1;

INSERT INTO notification_events (id, event_type, request_type, request_id, payload)
SELECT 'migration-15-loan-request-removed-event-' || member_id,
       'loan_request_removed_for_type_selection',
       'loan',
       '',
       '{}'
FROM migration_15_pending_loan_members;

INSERT INTO notifications (id, event_id, user_id, title_key, body_key, link, audience)
SELECT 'migration-15-loan-request-removed-notification-' || pending.member_id,
       'migration-15-loan-request-removed-event-' || pending.member_id,
       users.id,
       'notification_loan_request_removed_title',
       'notification_loan_request_removed_body',
       '/member/loan-requests',
       'member'
FROM migration_15_pending_loan_members pending
INNER JOIN users
    ON users.member_id = pending.member_id
   AND users.historical_identity = FALSE;

DELETE FROM notifications
WHERE audience = 'officer'
  AND resolved_at IS NULL
  AND event_id IN (
      SELECT id
      FROM notification_events
      WHERE request_type = 'loan'
        AND request_id IN (SELECT id FROM loan_requests WHERE status = 'pending')
  );

DELETE FROM loan_request_approvals
WHERE request_id IN (SELECT id FROM loan_requests WHERE status = 'pending');

DELETE FROM loan_requests
WHERE status = 'pending';

ALTER TABLE loan_requests
    ALTER COLUMN loan_type SET NOT NULL;
ALTER TABLE loans
    ALTER COLUMN loan_type SET NOT NULL;

INSERT INTO schema_migrations (version, name)
VALUES (15, 'classify_legacy_loans_and_remove_pending_requests');

COMMIT;
