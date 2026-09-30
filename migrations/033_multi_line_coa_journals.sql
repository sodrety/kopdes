BEGIN;

ALTER TABLE accounting_mappings ADD COLUMN category TEXT NOT NULL DEFAULT '';
UPDATE accounting_mappings
SET mapping_key = transaction_type || '|' || component || '|' || category || '|' || loan_type || '|' || COALESCE(effective_from,'') || '|' || COALESCE(effective_to,'');
ALTER TABLE financial_journal_entries ADD COLUMN category TEXT NOT NULL DEFAULT '';
ALTER TABLE financial_journal_entries ADD COLUMN loan_type TEXT NOT NULL DEFAULT '';
ALTER TABLE financial_journal_entries ADD COLUMN reversal_of TEXT NULL REFERENCES financial_journal_entries(id);
ALTER TABLE financial_journal_entries ADD COLUMN correction_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE financial_journal_lines ADD COLUMN mapping_override BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE loan_requests ADD COLUMN proposed_cash_coa_code TEXT NOT NULL DEFAULT '';
ALTER TABLE loan_requests ADD COLUMN proposed_loan_coa_code TEXT NOT NULL DEFAULT '';
ALTER TABLE loan_requests ADD COLUMN proposed_admin_fee_coa_code TEXT NOT NULL DEFAULT '';
ALTER TABLE withdrawal_requests ADD COLUMN proposed_cash_coa_code TEXT NOT NULL DEFAULT '';
ALTER TABLE withdrawal_requests ADD COLUMN proposed_savings_coa_code TEXT NOT NULL DEFAULT '';
INSERT INTO coa_accounts (id,code,name,account_type,subtype,normal_balance,is_group,active,system_key)
VALUES ('coa-primary-sales-income','40101','HASIL USAHA PENJUALAN PRIMER','revenue','Laba Rugi', 'C',FALSE,TRUE,NULL)
ON CONFLICT (code) DO NOTHING;

INSERT INTO accounting_mappings (id,mapping_key,transaction_type,component,category,loan_type,coa_code,active)
SELECT seed.id,seed.mapping_key,seed.transaction_type,seed.component,seed.category,seed.loan_type,seed.coa_code,TRUE
FROM (
    SELECT 'seed-map-savings-cash-bank' AS id,'savings|cash_bank||||' AS mapping_key,'savings' AS transaction_type,'cash_bank' AS component,'' AS category,'' AS loan_type,'10200' AS coa_code
    UNION ALL
    SELECT 'seed-map-savings-pokok','savings|savings_liability|pokok|||','savings','savings_liability','pokok','','31001'
    UNION ALL
    SELECT 'seed-map-savings-wajib','savings|savings_liability|wajib|||','savings','savings_liability','wajib','','31002'
    UNION ALL
    SELECT 'seed-map-savings-sukarela','savings|savings_liability|sukarela|||','savings','savings_liability','sukarela','','31003'
    UNION ALL
    SELECT 'seed-map-savings-shu','savings|savings_liability|shu|||','savings','savings_liability','shu','','31004'
    UNION ALL
    SELECT 'seed-map-savings-khusus','savings|savings_liability|khusus|||','savings','savings_liability','khusus','','31004'
    UNION ALL
    SELECT 'seed-map-withdrawal-cash-bank','withdrawal|cash_bank||||','withdrawal','cash_bank','','','10200'
    UNION ALL
    SELECT 'seed-map-withdrawal-pokok','withdrawal|savings_liability|pokok|||','withdrawal','savings_liability','pokok','','31001'
    UNION ALL
    SELECT 'seed-map-withdrawal-wajib','withdrawal|savings_liability|wajib|||','withdrawal','savings_liability','wajib','','31002'
    UNION ALL
    SELECT 'seed-map-withdrawal-sukarela','withdrawal|savings_liability|sukarela|||','withdrawal','savings_liability','sukarela','','31003'
    UNION ALL
    SELECT 'seed-map-withdrawal-shu','withdrawal|savings_liability|shu|||','withdrawal','savings_liability','shu','','31004'
    UNION ALL
    SELECT 'seed-map-withdrawal-khusus','withdrawal|savings_liability|khusus|||','withdrawal','savings_liability','khusus','','31004'
    UNION ALL
    SELECT 'seed-map-loan-cash-bank','loan|cash_bank||||','loan','cash_bank','','','10200'
    UNION ALL
    SELECT 'seed-map-loan-regular-receivable','loan|loan_receivable||regular||','loan','loan_receivable','','regular','14001'
    UNION ALL
    SELECT 'seed-map-loan-secondary-receivable','loan|loan_receivable||secondary_goods||','loan','loan_receivable','','secondary_goods','11102'
    UNION ALL
    SELECT 'seed-map-loan-paylater-receivable','loan|loan_receivable||goods_purchase_paylater||','loan','loan_receivable','','goods_purchase_paylater','11101'
    UNION ALL
    SELECT 'seed-map-loan-regular-fee','loan|admin_fee_income||regular||','loan','admin_fee_income','','regular','40201'
    UNION ALL
    SELECT 'seed-map-loan-secondary-income','loan|admin_fee_income||secondary_goods||','loan','admin_fee_income','','secondary_goods','40100'
    UNION ALL
    SELECT 'seed-map-loan-primary-income','loan|admin_fee_income||goods_purchase_paylater||','loan','admin_fee_income','','goods_purchase_paylater','40101'
    UNION ALL
    SELECT 'seed-map-repayment-cash-bank','repayment|cash_bank||||','repayment','cash_bank','','','10200'
    UNION ALL
    SELECT 'seed-map-repayment-regular-receivable','repayment|loan_receivable||regular||','repayment','loan_receivable','','regular','14001'
    UNION ALL
    SELECT 'seed-map-repayment-secondary-receivable','repayment|loan_receivable||secondary_goods||','repayment','loan_receivable','','secondary_goods','11102'
    UNION ALL
    SELECT 'seed-map-repayment-paylater-receivable','repayment|loan_receivable||goods_purchase_paylater||','repayment','loan_receivable','','goods_purchase_paylater','11101'
    UNION ALL
    SELECT 'seed-map-manual-cash-bank','manual|cash_bank||||','manual','cash_bank','','','10200'
) AS seed
JOIN coa_accounts coa ON coa.code=seed.coa_code
WHERE coa.active=TRUE AND coa.is_group=FALSE
ON CONFLICT (mapping_key) DO NOTHING;

CREATE INDEX idx_accounting_mappings_category_lookup
    ON accounting_mappings(transaction_type,component,category,loan_type,effective_from,effective_to,active);
CREATE INDEX idx_financial_journal_entries_status_date
    ON financial_journal_entries(status,transaction_date);
CREATE INDEX idx_financial_journal_entries_reversal
    ON financial_journal_entries(reversal_of);

CREATE TABLE accounting_backfill_runs (
    name TEXT PRIMARY KEY,
    completed_at TIMESTAMP NULL
);

INSERT INTO schema_migrations (version,name)
VALUES (33,'multi_line_coa_journals');

COMMIT;
