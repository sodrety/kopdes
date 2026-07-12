-- Keep the oldest pending request per member; deterministically reject later
-- duplicates so populated databases can install the invariant safely.
UPDATE loan_requests SET status = 'rejected', updated_at = CURRENT_TIMESTAMP
WHERE id IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY created_at, id) AS sequence
        FROM loan_requests WHERE status = 'pending'
    ) ranked WHERE sequence > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_loan_requests_one_pending_per_member
ON loan_requests(member_id)
WHERE status = 'pending';
