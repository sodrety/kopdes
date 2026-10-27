BEGIN;
SET LOCAL statement_timeout = '30s';
SET LOCAL search_path = public, pg_temp;

CREATE OR REPLACE VIEW public.member_loan_amount_limits AS
    SELECT m.id AS member_id,
        (CASE m.member_type WHEN 'contract_worker' THEN 2 WHEN 'employee' THEN 4 WHEN 'daily_worker' THEN 1 WHEN 'customer' THEN 1 ELSE 0 END)
            * (
                GREATEST(COALESCE(SUM(CASE WHEN sr.category='wajib' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='wajib' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0)
                + GREATEST(COALESCE(SUM(CASE WHEN sr.category='sukarela' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='sukarela' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0)
            ) AS loan_limit
    FROM public.members m LEFT JOIN public.saving_records sr ON sr.member_id=m.id
    GROUP BY m.id,m.member_type;

COMMIT;
