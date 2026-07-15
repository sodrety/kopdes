BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE loan_requests ALTER COLUMN requested_amount TYPE BIGINT;
ALTER TABLE saving_records ALTER COLUMN amount TYPE BIGINT;
ALTER TABLE withdrawal_requests ALTER COLUMN amount TYPE BIGINT;
ALTER TABLE withdrawal_reservations ALTER COLUMN amount TYPE BIGINT;
ALTER TABLE loan_requests ALTER COLUMN proposed_approved_amount TYPE BIGINT;
ALTER TABLE loans ALTER COLUMN approved_amount TYPE BIGINT;
ALTER TABLE loans ALTER COLUMN monthly_installment TYPE BIGINT;
ALTER TABLE loans ALTER COLUMN remaining_balance TYPE BIGINT;
ALTER TABLE loans ALTER COLUMN total_interest TYPE BIGINT;
ALTER TABLE loans ALTER COLUMN total_obligation TYPE BIGINT;
ALTER TABLE loan_installments ALTER COLUMN scheduled_amount TYPE BIGINT;
ALTER TABLE loan_installments ALTER COLUMN paid_amount TYPE BIGINT;
ALTER TABLE loan_repayments ALTER COLUMN amount TYPE BIGINT;

ALTER TABLE loan_requests ADD COLUMN proposed_admin_fee_policy TEXT NULL;
ALTER TABLE loan_requests ADD COLUMN proposed_monthly_admin_fee BIGINT NULL;
ALTER TABLE loan_requests ADD COLUMN proposed_total_admin_fee BIGINT NULL;
ALTER TABLE loan_requests ADD COLUMN proposed_total_obligation BIGINT NULL;

ALTER TABLE loans ADD COLUMN admin_fee_policy TEXT NULL;
ALTER TABLE loans ADD COLUMN monthly_admin_fee BIGINT NULL;
ALTER TABLE loans ADD COLUMN total_admin_fee BIGINT NULL;

UPDATE loans
SET admin_fee_policy = 'legacy_flat_monthly',
    monthly_admin_fee = NULL,
    total_admin_fee = total_interest;

ALTER TABLE loans ALTER COLUMN admin_fee_policy SET NOT NULL;
ALTER TABLE loans ALTER COLUMN total_admin_fee SET NOT NULL;
ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_check CHECK (admin_fee_policy IN ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','legacy_flat_monthly'));
ALTER TABLE loans ADD CONSTRAINT loans_monthly_admin_fee_check CHECK (monthly_admin_fee IS NULL OR monthly_admin_fee >= 0);
ALTER TABLE loans ADD CONSTRAINT loans_total_admin_fee_check CHECK (total_admin_fee >= 0);
ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_fields_check CHECK (
    (admin_fee_policy = 'regular_tiered_monthly_v1'
      AND approved_amount > 0 AND duration_months BETWEEN 1 AND 24
      AND monthly_admin_fee IS NOT NULL
      AND monthly_admin_fee = CASE WHEN approved_amount <= 25000000 THEN (approved_amount + 50) / 100
          ELSE 250000 + ((approved_amount - 25000000) / 200) * 3 + ((((approved_amount - 25000000) % 200) * 3 + 100) / 200) END
      AND total_admin_fee IS NOT NULL AND total_admin_fee % NULLIF(duration_months, 0) = 0 AND total_admin_fee / NULLIF(duration_months, 0) = monthly_admin_fee
      AND total_obligation IS NOT NULL AND total_obligation >= approved_amount AND total_obligation - approved_amount = total_admin_fee)
    OR (admin_fee_policy = 'secondary_goods_one_time_v1'
      AND approved_amount > 0 AND duration_months BETWEEN 1 AND 12
      AND monthly_admin_fee IS NULL
      AND total_admin_fee = (approved_amount * 20 + 50) / 100
      AND total_obligation IS NOT NULL AND total_obligation - approved_amount = total_admin_fee)
    OR (admin_fee_policy = 'goods_purchase_paylater_one_time_v1'
      AND approved_amount > 0 AND duration_months = 1
      AND monthly_admin_fee IS NULL
      AND total_admin_fee = (approved_amount * 5 + 50) / 100
      AND total_obligation IS NOT NULL AND total_obligation - approved_amount = total_admin_fee)
    OR (admin_fee_policy = 'legacy_flat_monthly' AND monthly_admin_fee IS NULL AND total_admin_fee IS NOT NULL AND total_obligation IS NOT NULL)
);
ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_identity_check CHECK (
    (loan_type='regular' AND legacy_terms=FALSE AND admin_fee_policy='regular_tiered_monthly_v1') OR
    (loan_type='secondary_goods' AND legacy_terms=FALSE AND admin_fee_policy='secondary_goods_one_time_v1') OR
    (loan_type='goods_purchase_paylater' AND legacy_terms=FALSE AND admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
    (loan_type='regular' AND legacy_terms=TRUE AND admin_fee_policy='legacy_flat_monthly')
);

ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_policy_check CHECK (proposed_admin_fee_policy IS NULL OR proposed_admin_fee_policy IN ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','legacy_flat_monthly'));
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_monthly_admin_fee_check CHECK (proposed_monthly_admin_fee IS NULL OR proposed_monthly_admin_fee >= 0);
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_total_admin_fee_check CHECK (proposed_total_admin_fee IS NULL OR proposed_total_admin_fee >= 0);
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_total_obligation_check CHECK (proposed_total_obligation IS NULL OR proposed_total_obligation > 0);
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_fields_check CHECK (
    (proposed_admin_fee_policy IS NULL AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NULL AND proposed_total_obligation IS NULL)
    OR (proposed_admin_fee_policy = 'regular_tiered_monthly_v1'
      AND proposed_approved_amount > 0 AND proposed_duration_months BETWEEN 1 AND 24
      AND proposed_monthly_admin_fee IS NOT NULL
      AND proposed_monthly_admin_fee = CASE WHEN proposed_approved_amount <= 25000000 THEN (proposed_approved_amount + 50) / 100
          ELSE 250000 + ((proposed_approved_amount - 25000000) / 200) * 3 + ((((proposed_approved_amount - 25000000) % 200) * 3 + 100) / 200) END
      AND proposed_total_admin_fee IS NOT NULL AND proposed_total_admin_fee % NULLIF(proposed_duration_months, 0) = 0 AND proposed_total_admin_fee / NULLIF(proposed_duration_months, 0) = proposed_monthly_admin_fee
      AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation >= proposed_approved_amount AND proposed_total_obligation - proposed_approved_amount = proposed_total_admin_fee)
    OR (proposed_admin_fee_policy = 'secondary_goods_one_time_v1'
      AND proposed_approved_amount > 0 AND proposed_duration_months BETWEEN 1 AND 12
      AND proposed_monthly_admin_fee IS NULL
      AND proposed_total_admin_fee = (proposed_approved_amount * 20 + 50) / 100
      AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation - proposed_approved_amount = proposed_total_admin_fee)
    OR (proposed_admin_fee_policy = 'goods_purchase_paylater_one_time_v1'
      AND proposed_approved_amount > 0 AND proposed_duration_months = 1
      AND proposed_monthly_admin_fee IS NULL
      AND proposed_total_admin_fee = (proposed_approved_amount * 5 + 50) / 100
      AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation - proposed_approved_amount = proposed_total_admin_fee)
    OR (proposed_admin_fee_policy = 'legacy_flat_monthly' AND proposed_approved_amount IS NOT NULL AND proposed_duration_months IS NOT NULL AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NOT NULL AND proposed_total_obligation IS NOT NULL)
);
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_policy_identity_check CHECK (
    proposed_admin_fee_policy IS NULL OR
    (loan_type='regular' AND legacy_terms=FALSE AND proposed_admin_fee_policy='regular_tiered_monthly_v1') OR
    (loan_type='secondary_goods' AND legacy_terms=FALSE AND proposed_admin_fee_policy='secondary_goods_one_time_v1') OR
    (loan_type='goods_purchase_paylater' AND legacy_terms=FALSE AND proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
    (loan_type='regular' AND legacy_terms=TRUE AND proposed_admin_fee_policy='legacy_flat_monthly')
);

CREATE FUNCTION protect_loan_admin_fee_snapshot() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF OLD.admin_fee_policy IS DISTINCT FROM NEW.admin_fee_policy
       OR OLD.monthly_admin_fee IS DISTINCT FROM NEW.monthly_admin_fee
       OR OLD.total_admin_fee IS DISTINCT FROM NEW.total_admin_fee
       OR OLD.total_obligation IS DISTINCT FROM NEW.total_obligation
       OR OLD.approved_amount IS DISTINCT FROM NEW.approved_amount
       OR OLD.duration_months IS DISTINCT FROM NEW.duration_months THEN
        RAISE EXCEPTION 'loan admin fee snapshot is immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_loan_admin_fee_snapshot BEFORE UPDATE OF admin_fee_policy, monthly_admin_fee, total_admin_fee, total_obligation, approved_amount, duration_months ON loans FOR EACH ROW EXECUTE FUNCTION protect_loan_admin_fee_snapshot();

CREATE FUNCTION protect_proposed_loan_admin_fee_snapshot() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF OLD.proposed_admin_fee_policy IS NOT NULL AND (
       OLD.proposed_admin_fee_policy IS DISTINCT FROM NEW.proposed_admin_fee_policy
       OR OLD.proposed_monthly_admin_fee IS DISTINCT FROM NEW.proposed_monthly_admin_fee
       OR OLD.proposed_total_admin_fee IS DISTINCT FROM NEW.proposed_total_admin_fee
       OR OLD.proposed_total_obligation IS DISTINCT FROM NEW.proposed_total_obligation
       OR OLD.proposed_approved_amount IS DISTINCT FROM NEW.proposed_approved_amount
       OR OLD.proposed_duration_months IS DISTINCT FROM NEW.proposed_duration_months) THEN
        RAISE EXCEPTION 'proposed loan admin fee snapshot is immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_proposed_loan_admin_fee_snapshot BEFORE UPDATE OF proposed_admin_fee_policy, proposed_monthly_admin_fee, proposed_total_admin_fee, proposed_total_obligation, proposed_approved_amount, proposed_duration_months ON loan_requests FOR EACH ROW EXECUTE FUNCTION protect_proposed_loan_admin_fee_snapshot();

CREATE FUNCTION protect_loan_terms_identity() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'loan terms identity is immutable after snapshot';
END $$;
CREATE TRIGGER protect_loan_terms_identity BEFORE UPDATE OF loan_type, legacy_terms ON loans FOR EACH ROW EXECUTE FUNCTION protect_loan_terms_identity();

CREATE FUNCTION protect_proposed_loan_terms_identity() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF OLD.proposed_admin_fee_policy IS NOT NULL AND
       (OLD.loan_type IS DISTINCT FROM NEW.loan_type OR OLD.legacy_terms IS DISTINCT FROM NEW.legacy_terms) THEN
        RAISE EXCEPTION 'loan terms identity is immutable after snapshot';
    END IF;
    IF OLD.proposed_admin_fee_policy IS NULL AND NEW.proposed_admin_fee_policy IS NOT NULL AND
       (OLD.status <> 'pending' OR OLD.current_approval_stage <> 'manager') THEN
        RAISE EXCEPTION 'proposed loan admin fee snapshot must be assigned at Manager stage';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_proposed_loan_terms_identity BEFORE UPDATE OF loan_type, legacy_terms, proposed_admin_fee_policy ON loan_requests FOR EACH ROW EXECUTE FUNCTION protect_proposed_loan_terms_identity();
CREATE FUNCTION validate_proposed_loan_snapshot_insert() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.status <> 'pending' OR NEW.current_approval_stage <> 'manager') THEN
        RAISE EXCEPTION 'proposed loan admin fee snapshot must be assigned at Manager stage';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER validate_proposed_loan_snapshot_insert BEFORE INSERT ON loan_requests FOR EACH ROW EXECUTE FUNCTION validate_proposed_loan_snapshot_insert();

CREATE FUNCTION validate_loan_request_origin_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.legacy_terms IS DISTINCT FROM FALSE
        OR NEW.status <> 'pending'
        OR NEW.current_approval_stage <> 'manager'
        OR NEW.proposed_admin_fee_policy IS NOT NULL
        OR NEW.proposed_monthly_admin_fee IS NOT NULL
        OR NEW.proposed_total_admin_fee IS NOT NULL
        OR NEW.proposed_total_obligation IS NOT NULL
        OR NEW.proposed_approved_amount IS NOT NULL
        OR NEW.proposed_duration_months IS NOT NULL
        OR NEW.proposed_start_date <> '' THEN
        RAISE EXCEPTION 'new loan requests must begin nonlegacy at Manager stage';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER validate_loan_request_origin_insert BEFORE INSERT ON loan_requests FOR EACH ROW EXECUTE FUNCTION validate_loan_request_origin_insert();

CREATE FUNCTION protect_loan_request_legacy_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.legacy_terms = FALSE AND NEW.legacy_terms <> FALSE THEN
        RAISE EXCEPTION 'legacy loan terms are migration-only';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_loan_request_legacy_provenance BEFORE UPDATE OF legacy_terms ON loan_requests FOR EACH ROW EXECUTE FUNCTION protect_loan_request_legacy_provenance();

CREATE FUNCTION protect_loan_legacy_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.legacy_terms <> FALSE THEN
        RAISE EXCEPTION 'legacy loan terms are migration-only';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_loan_legacy_provenance BEFORE INSERT ON loans FOR EACH ROW WHEN (NEW.legacy_terms <> FALSE) EXECUTE FUNCTION protect_loan_legacy_provenance();

CREATE FUNCTION validate_loan_request_state_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    approved_current BOOLEAN;
    approved_manager BOOLEAN;
BEGIN
    IF OLD.legacy_terms = FALSE THEN
        IF NEW.legacy_terms <> FALSE THEN
            RAISE EXCEPTION 'legacy loan terms are migration-only';
        END IF;
        IF NEW.status = 'pending' AND (NEW.current_approval_stage IS NULL OR NEW.current_approval_stage NOT IN ('manager','ketua_i','ketua_ii','ketua_utama') OR (NEW.current_approval_stage <> 'manager' AND NEW.proposed_admin_fee_policy IS NULL)) THEN
            RAISE EXCEPTION 'invalid loan request approval state';
        END IF;
        IF NEW.status = 'approved' AND (NEW.current_approval_stage IS NOT NULL OR NEW.proposed_admin_fee_policy IS NULL) THEN
            RAISE EXCEPTION 'invalid approved loan request state';
        END IF;
        IF NEW.status IN ('rejected','cancelled') AND NEW.current_approval_stage IS NOT NULL THEN
            RAISE EXCEPTION 'invalid terminal loan request state';
        END IF;
        SELECT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id = OLD.id AND a.stage = OLD.current_approval_stage AND a.decision = 'approved') INTO approved_current;
        IF NOT (
            (OLD.status = 'pending' AND NEW.status = 'pending' AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
            OR (OLD.status = 'pending' AND NEW.status = 'pending' AND ((OLD.current_approval_stage = 'manager' AND NEW.current_approval_stage = 'ketua_i') OR (OLD.current_approval_stage = 'ketua_i' AND NEW.current_approval_stage = 'ketua_ii') OR (OLD.current_approval_stage = 'ketua_ii' AND NEW.current_approval_stage = 'ketua_utama')) AND approved_current)
            OR (OLD.status = 'pending' AND NEW.status = 'approved' AND OLD.current_approval_stage = 'ketua_utama' AND approved_current)
            OR (OLD.status = 'pending' AND NEW.status = 'rejected' AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id = OLD.id AND a.stage = OLD.current_approval_stage AND a.decision = 'rejected'))
            OR (OLD.status = 'pending' AND NEW.status = 'cancelled')
            OR (OLD.status <> 'pending' AND OLD.status = NEW.status AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
        ) THEN
            RAISE EXCEPTION 'invalid loan request state transition';
        END IF;
        SELECT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id = NEW.id AND a.stage = 'manager' AND a.decision = 'approved') INTO approved_manager;
        IF NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.current_approval_stage <> 'manager' OR NEW.status <> 'pending') AND NOT approved_manager THEN
            RAISE EXCEPTION 'Manager approval is required for snapshotted terms';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER validate_loan_request_state_integrity BEFORE UPDATE OF status, current_approval_stage, legacy_terms, proposed_admin_fee_policy, proposed_monthly_admin_fee, proposed_total_admin_fee, proposed_total_obligation, proposed_approved_amount, proposed_duration_months, proposed_start_date ON loan_requests FOR EACH ROW EXECUTE FUNCTION validate_loan_request_state_integrity();

CREATE FUNCTION validate_loan_request_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.legacy_terms = FALSE AND (
        NOT EXISTS (SELECT 1 FROM loan_requests r WHERE r.id = NEW.loan_request_id AND r.status = 'approved' AND r.current_approval_stage IS NULL AND r.legacy_terms = FALSE AND r.member_id = NEW.member_id AND r.loan_type = NEW.loan_type AND r.proposed_approved_amount = NEW.approved_amount AND r.proposed_duration_months = NEW.duration_months AND r.proposed_start_date = NEW.start_date AND r.proposed_admin_fee_policy = NEW.admin_fee_policy AND r.proposed_monthly_admin_fee IS NOT DISTINCT FROM NEW.monthly_admin_fee AND r.proposed_total_admin_fee = NEW.total_admin_fee AND r.proposed_total_obligation = NEW.total_obligation)
        OR NEW.remaining_balance <> NEW.total_obligation
        OR (SELECT COUNT(*) FROM loan_request_approvals a WHERE a.request_id = NEW.loan_request_id AND a.decision = 'approved' AND a.stage IN ('manager','ketua_i','ketua_ii','ketua_utama')) <> 4
        OR NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id = NEW.loan_request_id AND a.stage = 'ketua_utama' AND a.decision = 'approved' AND a.officer_id = NEW.approved_by)
    ) THEN
        RAISE EXCEPTION 'loan must match a fully approved loan request';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER validate_loan_request_provenance BEFORE INSERT ON loans FOR EACH ROW EXECUTE FUNCTION validate_loan_request_provenance();

CREATE TABLE monetary_aggregate_totals (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    saving_records_total BIGINT NOT NULL CHECK (saving_records_total >= 0),
    withdrawal_requests_total BIGINT NOT NULL CHECK (withdrawal_requests_total >= 0),
    withdrawal_reservations_total BIGINT NOT NULL CHECK (withdrawal_reservations_total >= 0),
    loan_requests_requested_total BIGINT NOT NULL CHECK (loan_requests_requested_total >= 0),
    loans_approved_total BIGINT NOT NULL CHECK (loans_approved_total >= 0),
    loans_obligation_total BIGINT NOT NULL CHECK (loans_obligation_total >= 0),
    loans_remaining_total BIGINT NOT NULL CHECK (loans_remaining_total >= 0),
    loan_repayments_total BIGINT NOT NULL CHECK (loan_repayments_total >= 0),
    loan_installments_scheduled_total BIGINT NOT NULL CHECK (loan_installments_scheduled_total >= 0),
    loan_installments_paid_total BIGINT NOT NULL CHECK (loan_installments_paid_total >= 0)
);
INSERT INTO monetary_aggregate_totals SELECT TRUE,
       (SELECT COALESCE(SUM(amount::NUMERIC), 0) FROM saving_records),
       (SELECT COALESCE(SUM(amount::NUMERIC), 0) FROM withdrawal_requests),
       (SELECT COALESCE(SUM(amount::NUMERIC), 0) FROM withdrawal_reservations),
       (SELECT COALESCE(SUM(requested_amount::NUMERIC), 0) FROM loan_requests),
       (SELECT COALESCE(SUM(approved_amount::NUMERIC), 0) FROM loans),
       (SELECT COALESCE(SUM(total_obligation::NUMERIC), 0) FROM loans),
       (SELECT COALESCE(SUM(remaining_balance::NUMERIC), 0) FROM loans),
       (SELECT COALESCE(SUM(amount::NUMERIC), 0) FROM loan_repayments),
       (SELECT COALESCE(SUM(scheduled_amount::NUMERIC), 0) FROM loan_installments),
       (SELECT COALESCE(SUM(paid_amount::NUMERIC), 0) FROM loan_installments);

CREATE FUNCTION maintain_monetary_aggregate_total() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    aggregate_column TEXT := TG_ARGV[0];
    value_column TEXT := TG_ARGV[1];
    old_value NUMERIC;
    new_value NUMERIC;
    delta NUMERIC;
    changed BIGINT;
BEGIN
    IF aggregate_column NOT IN ('saving_records_total', 'withdrawal_requests_total', 'withdrawal_reservations_total', 'loan_requests_requested_total', 'loans_approved_total', 'loans_obligation_total', 'loans_remaining_total', 'loan_repayments_total', 'loan_installments_scheduled_total', 'loan_installments_paid_total') THEN
        RAISE EXCEPTION 'invalid monetary aggregate column';
    END IF;
    IF TG_OP <> 'INSERT' THEN old_value := (to_jsonb(OLD)->>value_column)::NUMERIC; END IF;
    IF TG_OP <> 'DELETE' THEN new_value := (to_jsonb(NEW)->>value_column)::NUMERIC; END IF;
    IF TG_OP = 'INSERT' THEN delta := new_value;
    ELSIF TG_OP = 'DELETE' THEN delta := -old_value;
    ELSE delta := new_value - old_value;
    END IF;
    EXECUTE format('UPDATE %I.monetary_aggregate_totals SET %I=%I+$1 WHERE singleton=TRUE AND (($1>=0 AND %I<=9223372036854775807-$1) OR ($1<0 AND %I>=-$1))', TG_TABLE_SCHEMA, aggregate_column, aggregate_column, aggregate_column, aggregate_column) USING delta;
    GET DIAGNOSTICS changed = ROW_COUNT;
    IF changed <> 1 THEN
        RAISE EXCEPTION USING ERRCODE = '22003', MESSAGE = 'monetary aggregate capacity exceeded';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER saving_records_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF amount ON saving_records FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('saving_records_total','amount');
CREATE TRIGGER withdrawal_requests_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF amount ON withdrawal_requests FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('withdrawal_requests_total','amount');
CREATE TRIGGER withdrawal_reservations_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF amount ON withdrawal_reservations FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('withdrawal_reservations_total','amount');
CREATE TRIGGER loan_requests_requested_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF requested_amount ON loan_requests FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('loan_requests_requested_total','requested_amount');
CREATE TRIGGER loans_approved_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF approved_amount ON loans FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('loans_approved_total','approved_amount');
CREATE TRIGGER loans_obligation_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF total_obligation ON loans FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('loans_obligation_total','total_obligation');
CREATE TRIGGER loans_remaining_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF remaining_balance ON loans FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('loans_remaining_total','remaining_balance');
CREATE TRIGGER loan_repayments_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF amount ON loan_repayments FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('loan_repayments_total','amount');
CREATE TRIGGER loan_installments_scheduled_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF scheduled_amount ON loan_installments FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('loan_installments_scheduled_total','scheduled_amount');
CREATE TRIGGER loan_installments_paid_aggregate_total BEFORE INSERT OR DELETE OR UPDATE OF paid_amount ON loan_installments FOR EACH ROW EXECUTE FUNCTION maintain_monetary_aggregate_total('loan_installments_paid_total','paid_amount');

INSERT INTO schema_migrations (version, name)
VALUES (16, 'add_regular_loan_admin_fee_terms');

COMMIT;
