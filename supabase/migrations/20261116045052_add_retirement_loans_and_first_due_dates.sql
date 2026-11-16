-- Add Retirement as an admin-entered manual-fee loan with one repayment.
-- Persist an optional admin-selected first installment due date.

BEGIN;
SET LOCAL statement_timeout = '30s';
SET LOCAL search_path = public, pg_temp;

ALTER TABLE loan_requests ADD COLUMN first_due_date TEXT NOT NULL DEFAULT '' CHECK (first_due_date='' OR first_due_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$');

ALTER TABLE loan_request_batch_rows ADD COLUMN first_due_date TEXT NOT NULL DEFAULT '' CHECK (first_due_date='' OR first_due_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$');

ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_loan_type_check;

ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_loan_type_check CHECK (loan_type IN ('regular','secondary_goods','goods_purchase_paylater','voucher','retirement'));

ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_loan_type_check;

ALTER TABLE loans ADD CONSTRAINT loans_loan_type_check CHECK (loan_type IN ('regular','secondary_goods','goods_purchase_paylater','voucher','retirement'));

ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_admin_fee_policy_check;

ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_check CHECK (admin_fee_policy IN ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','voucher_nominal_one_time_v1','retirement_manual_one_time_v1','legacy_flat_monthly'));

ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_admin_fee_policy_check;

ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_policy_check CHECK (proposed_admin_fee_policy IS NULL OR proposed_admin_fee_policy IN ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','voucher_nominal_one_time_v1','retirement_manual_one_time_v1','legacy_flat_monthly'));

ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_admin_fee_policy_fields_check;

ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_fields_check CHECK (
    (admin_fee_policy='regular_tiered_monthly_v1' AND approved_amount>0 AND duration_months BETWEEN 1 AND 24 AND monthly_admin_fee IS NOT NULL
      AND monthly_admin_fee=CASE WHEN approved_amount<=25000000 THEN (approved_amount+50)/100 ELSE 250000+((approved_amount-25000000)/200)*3+((((approved_amount-25000000)%200)*3+100)/200) END
      AND total_admin_fee IS NOT NULL AND total_admin_fee%NULLIF(duration_months,0)=0 AND total_admin_fee/NULLIF(duration_months,0)=monthly_admin_fee
      AND total_obligation IS NOT NULL AND total_obligation>=approved_amount AND total_obligation-approved_amount=total_admin_fee)
    OR (admin_fee_policy='secondary_goods_one_time_v1' AND approved_amount>0 AND duration_months BETWEEN 1 AND 12 AND monthly_admin_fee IS NULL
      AND total_admin_fee=(approved_amount*20+50)/100 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee)
    OR (admin_fee_policy='goods_purchase_paylater_one_time_v1' AND approved_amount>0 AND duration_months=1 AND monthly_admin_fee IS NULL
      AND total_admin_fee=(approved_amount*5+50)/100 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee)
    OR (admin_fee_policy='legacy_flat_monthly' AND monthly_admin_fee IS NULL AND total_admin_fee IS NOT NULL AND total_obligation IS NOT NULL)
    OR (loan_type='voucher' AND legacy_terms=FALSE AND admin_fee_policy='voucher_nominal_one_time_v1' AND approved_amount>0 AND duration_months=1 AND monthly_admin_fee IS NULL AND total_admin_fee IS NOT NULL AND total_admin_fee>=0 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee)
    OR (loan_type='retirement' AND legacy_terms=FALSE AND admin_fee_policy='retirement_manual_one_time_v1' AND approved_amount>0 AND duration_months=1 AND monthly_admin_fee IS NULL AND total_admin_fee IS NOT NULL AND total_admin_fee>=0 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee));

ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_admin_fee_fields_check;

ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_fields_check CHECK (
    (proposed_admin_fee_policy IS NULL AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NULL AND proposed_total_obligation IS NULL)
    OR (proposed_admin_fee_policy='regular_tiered_monthly_v1' AND proposed_approved_amount>0 AND proposed_duration_months BETWEEN 1 AND 24 AND proposed_monthly_admin_fee IS NOT NULL
      AND proposed_monthly_admin_fee=CASE WHEN proposed_approved_amount<=25000000 THEN (proposed_approved_amount+50)/100 ELSE 250000+((proposed_approved_amount-25000000)/200)*3+((((proposed_approved_amount-25000000)%200)*3+100)/200) END
      AND proposed_total_admin_fee IS NOT NULL AND proposed_total_admin_fee%NULLIF(proposed_duration_months,0)=0 AND proposed_total_admin_fee/NULLIF(proposed_duration_months,0)=proposed_monthly_admin_fee
      AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation>=proposed_approved_amount AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (proposed_admin_fee_policy='secondary_goods_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months BETWEEN 1 AND 12 AND proposed_monthly_admin_fee IS NULL
      AND proposed_total_admin_fee=(proposed_approved_amount*20+50)/100 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months=1 AND proposed_monthly_admin_fee IS NULL
      AND proposed_total_admin_fee=(proposed_approved_amount*5+50)/100 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (proposed_admin_fee_policy='legacy_flat_monthly' AND proposed_approved_amount IS NOT NULL AND proposed_duration_months IS NOT NULL AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NOT NULL AND proposed_total_obligation IS NOT NULL)
    OR (loan_type='voucher' AND legacy_terms=FALSE AND proposed_admin_fee_policy='voucher_nominal_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months=1 AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NOT NULL AND proposed_total_admin_fee>=0 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (loan_type='retirement' AND legacy_terms=FALSE AND proposed_admin_fee_policy='retirement_manual_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months=1 AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NOT NULL AND proposed_total_admin_fee>=0 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee));

ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_admin_fee_policy_identity_check;

ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_identity_check CHECK (
    (loan_type='regular' AND legacy_terms=FALSE AND admin_fee_policy='regular_tiered_monthly_v1') OR
    (loan_type='secondary_goods' AND legacy_terms=FALSE AND admin_fee_policy='secondary_goods_one_time_v1') OR
    (loan_type='goods_purchase_paylater' AND legacy_terms=FALSE AND admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
    (loan_type IN ('regular','secondary_goods','goods_purchase_paylater') AND legacy_terms=TRUE AND admin_fee_policy='legacy_flat_monthly') OR
    (loan_type='voucher' AND legacy_terms=FALSE AND admin_fee_policy='voucher_nominal_one_time_v1') OR
    (loan_type='retirement' AND legacy_terms=FALSE AND admin_fee_policy='retirement_manual_one_time_v1'));

ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_admin_fee_policy_identity_check;

ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_policy_identity_check CHECK (
    proposed_admin_fee_policy IS NULL OR
    (loan_type='regular' AND legacy_terms=FALSE AND proposed_admin_fee_policy='regular_tiered_monthly_v1') OR
    (loan_type='secondary_goods' AND legacy_terms=FALSE AND proposed_admin_fee_policy='secondary_goods_one_time_v1') OR
    (loan_type='goods_purchase_paylater' AND legacy_terms=FALSE AND proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
    (loan_type IN ('regular','secondary_goods','goods_purchase_paylater') AND legacy_terms=TRUE AND proposed_admin_fee_policy='legacy_flat_monthly') OR
    (loan_type='voucher' AND legacy_terms=FALSE AND proposed_admin_fee_policy='voucher_nominal_one_time_v1') OR
    (loan_type='retirement' AND legacy_terms=FALSE AND proposed_admin_fee_policy='retirement_manual_one_time_v1'));

CREATE OR REPLACE FUNCTION validate_loan_admin_fee_policy_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NOT ((NEW.legacy_terms=TRUE AND NEW.admin_fee_policy='legacy_flat_monthly') OR (NEW.legacy_terms=FALSE AND (
        (NEW.loan_type='regular' AND NEW.admin_fee_policy='regular_tiered_monthly_v1') OR
        (NEW.loan_type='secondary_goods' AND NEW.admin_fee_policy='secondary_goods_one_time_v1') OR
        (NEW.loan_type='goods_purchase_paylater' AND NEW.admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
        (NEW.loan_type='voucher' AND NEW.admin_fee_policy='voucher_nominal_one_time_v1') OR
        (NEW.loan_type='retirement' AND NEW.admin_fee_policy='retirement_manual_one_time_v1')))) THEN
        RAISE EXCEPTION 'loan admin fee policy does not match loan terms';
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION validate_proposed_loan_admin_fee_policy_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.proposed_admin_fee_policy IS NOT NULL AND NOT ((NEW.legacy_terms=TRUE AND NEW.proposed_admin_fee_policy='legacy_flat_monthly') OR (NEW.legacy_terms=FALSE AND (
        (NEW.loan_type='regular' AND NEW.proposed_admin_fee_policy='regular_tiered_monthly_v1') OR
        (NEW.loan_type='secondary_goods' AND NEW.proposed_admin_fee_policy='secondary_goods_one_time_v1') OR
        (NEW.loan_type='goods_purchase_paylater' AND NEW.proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
        (NEW.loan_type='voucher' AND NEW.proposed_admin_fee_policy='voucher_nominal_one_time_v1') OR
        (NEW.loan_type='retirement' AND NEW.proposed_admin_fee_policy='retirement_manual_one_time_v1')))) THEN
        RAISE EXCEPTION 'proposed loan admin fee policy does not match loan terms';
    END IF;
    RETURN NEW;
END $$;

ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_voucher_intake_check;

ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_manual_fee_intake_check CHECK (
    (loan_type='voucher' AND creation_source='admin' AND created_by IS NOT NULL AND batch_id IS NOT NULL AND legacy_terms=FALSE AND duration_months=1 AND requested_admin_fee IS NOT NULL AND requested_admin_fee>=0 AND requested_amount<=9223372036854775807-requested_admin_fee AND (proposed_admin_fee_policy IS NULL OR (proposed_admin_fee_policy='voucher_nominal_one_time_v1' AND proposed_total_admin_fee=requested_admin_fee))) OR
    (loan_type='retirement' AND creation_source='admin' AND created_by IS NOT NULL AND legacy_terms=FALSE AND duration_months=1 AND requested_admin_fee IS NOT NULL AND requested_admin_fee>=0 AND requested_amount<=9223372036854775807-requested_admin_fee AND (proposed_admin_fee_policy IS NULL OR (proposed_admin_fee_policy='retirement_manual_one_time_v1' AND proposed_total_admin_fee=requested_admin_fee))) OR
    (loan_type NOT IN ('voucher','retirement') AND requested_admin_fee IS NULL));

CREATE OR REPLACE FUNCTION protect_voucher_intake() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.first_due_date IS DISTINCT FROM OLD.first_due_date THEN
        RAISE EXCEPTION 'First due date is immutable';
    END IF;
    IF OLD.loan_type IN ('voucher','retirement') AND (NEW.requested_admin_fee IS DISTINCT FROM OLD.requested_admin_fee OR NEW.creation_source IS DISTINCT FROM OLD.creation_source OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.batch_id IS DISTINCT FROM OLD.batch_id) THEN
        RAISE EXCEPTION 'Manual-fee loan intake and provenance are immutable';
    END IF;
    RETURN NEW;
END $$;

INSERT INTO public.schema_migrations (version, name)
VALUES (53, 'add_retirement_loans_and_first_due_dates')
ON CONFLICT (version) DO NOTHING;

COMMIT;
