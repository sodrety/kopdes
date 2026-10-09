BEGIN;

ALTER TABLE loan_repayment_audits
    DROP CONSTRAINT IF EXISTS loan_repayment_audits_action_check;

ALTER TABLE loan_repayment_audits
    ADD CONSTRAINT loan_repayment_audits_action_check
    CHECK (action IN ('added','edited','removed'));

INSERT INTO schema_migrations (version, name)
VALUES (41, 'allow_added_repayment_audits');

COMMIT;
