BEGIN;

ALTER TABLE manual_cash_transaction_drafts
    DROP CONSTRAINT IF EXISTS manual_cash_transaction_drafts_check;

ALTER TABLE manual_cash_transaction_drafts
    ADD CONSTRAINT manual_cash_transaction_drafts_approval_state_check CHECK (
        (status='pending' AND approved_by IS NULL AND transaction_id IS NULL AND approved_at IS NULL)
        OR
        (status='approved' AND approved_by IS NOT NULL AND approved_at IS NOT NULL
            AND (entry_type='journal' OR transaction_id IS NOT NULL))
    );

INSERT INTO schema_migrations (version, name)
VALUES (39, 'allow_non_cash_general_journal_approval');

COMMIT;
