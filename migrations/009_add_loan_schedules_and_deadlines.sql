-- PostgreSQL migration. The Go runtime migration performs the equivalent
-- transactionally for both PostgreSQL and SQLite.
BEGIN;

ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_status_check;
ALTER TABLE loans ADD CONSTRAINT loans_status_check
    CHECK (status IN ('active', 'paid', 'cancelled', 'adjustment_due'));
ALTER TABLE loans ADD COLUMN start_date TEXT NOT NULL DEFAULT '';
ALTER TABLE loans ADD COLUMN interest_rate_bps INTEGER NOT NULL DEFAULT 100 CHECK (interest_rate_bps BETWEEN 0 AND 1000);
ALTER TABLE loans ADD COLUMN total_interest INTEGER NOT NULL DEFAULT 0 CHECK (total_interest >= 0);
ALTER TABLE loans ADD COLUMN total_obligation INTEGER NOT NULL DEFAULT 0 CHECK (total_obligation >= 0);
ALTER TABLE loans ADD COLUMN next_due_date TEXT NOT NULL DEFAULT '';
ALTER TABLE loans ADD COLUMN final_due_date TEXT NOT NULL DEFAULT '';

CREATE TABLE loan_installments (
    id TEXT PRIMARY KEY,
    loan_id TEXT NOT NULL REFERENCES loans(id),
    installment_no INTEGER NOT NULL CHECK (installment_no > 0),
    due_date TEXT NOT NULL,
    scheduled_amount INTEGER NOT NULL CHECK (scheduled_amount > 0),
    paid_amount INTEGER NOT NULL DEFAULT 0 CHECK (paid_amount BETWEEN 0 AND scheduled_amount),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (loan_id, installment_no)
);
CREATE INDEX idx_loan_installments_due ON loan_installments(loan_id, due_date);

CREATE TABLE loan_start_date_audits (
    id TEXT PRIMARY KEY,
    loan_id TEXT NOT NULL REFERENCES loans(id),
    old_start_date TEXT NOT NULL,
    new_start_date TEXT NOT NULL,
    changed_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

WITH calculated AS (
    SELECT l.id,
           (l.approved_at AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Jakarta')::date AS start_on,
           ROUND(l.approved_amount::numeric * 100 * l.duration_months / 10000)::bigint AS interest,
           l.approved_amount + ROUND(l.approved_amount::numeric * 100 * l.duration_months / 10000)::bigint AS obligation,
           COALESCE((SELECT SUM(lr.amount) FROM loan_repayments lr WHERE lr.loan_id = l.id), 0)::bigint AS repaid
    FROM loans l
    WHERE l.status <> 'cancelled'
), updated AS (
    UPDATE loans l
       SET start_date = c.start_on::text,
           interest_rate_bps = 100,
           total_interest = c.interest,
           total_obligation = c.obligation,
           monthly_installment = c.obligation / l.duration_months,
           remaining_balance = c.obligation - c.repaid,
           status = CASE WHEN c.obligation = c.repaid THEN 'paid'
                         WHEN l.status = 'paid' THEN 'adjustment_due'
                         ELSE l.status END,
           final_due_date = (date_trunc('month', c.start_on) + (l.duration_months || ' months')::interval
               + (LEAST(EXTRACT(day FROM c.start_on)::int,
                   EXTRACT(day FROM date_trunc('month', c.start_on) + ((l.duration_months + 1) || ' months')::interval - interval '1 day')::int) - 1) * interval '1 day')::date::text
      FROM calculated c WHERE c.id = l.id
    RETURNING l.id
)
INSERT INTO loan_installments (id, loan_id, installment_no, due_date, scheduled_amount, paid_amount)
SELECT l.id || '-' || lpad(g.n::text, 3, '0'), l.id, g.n,
       (date_trunc('month', c.start_on) + (g.n || ' months')::interval
         + (LEAST(EXTRACT(day FROM c.start_on)::int,
             EXTRACT(day FROM date_trunc('month', c.start_on) + ((g.n + 1) || ' months')::interval - interval '1 day')::int) - 1) * interval '1 day')::date::text,
       (c.obligation / l.duration_months) + CASE WHEN g.n = l.duration_months THEN c.obligation % l.duration_months ELSE 0 END,
       LEAST((c.obligation / l.duration_months) + CASE WHEN g.n = l.duration_months THEN c.obligation % l.duration_months ELSE 0 END,
             GREATEST(0, c.repaid - ((g.n - 1) * (c.obligation / l.duration_months))))
FROM loans l
JOIN calculated c ON c.id = l.id
CROSS JOIN LATERAL generate_series(1, l.duration_months) AS g(n);

-- Cancelled Loans are void obligations: no retroactive Bunga or schedule.
UPDATE loans
SET start_date = '',
    interest_rate_bps = 0,
    total_interest = 0,
    total_obligation = 0,
    remaining_balance = 0,
    next_due_date = '',
    final_due_date = ''
WHERE status = 'cancelled';

UPDATE loans l SET next_due_date = COALESCE((
    SELECT li.due_date FROM loan_installments li
    WHERE li.loan_id = l.id AND li.paid_amount < li.scheduled_amount
    ORDER BY li.installment_no LIMIT 1
), '');

COMMIT;
