-- PostgreSQL migration mirrored by runtime migration 23 in internal/app/migrations.go.
BEGIN;

ALTER TABLE officer_appointments
    DROP CONSTRAINT IF EXISTS officer_appointments_role_check;

ALTER TABLE officer_appointments
    ADD CONSTRAINT officer_appointments_role_check
    CHECK (role IN ('manager', 'bendahara', 'ketua_i', 'ketua_ii', 'ketua_utama'));

CREATE TABLE cash_transaction_categories (
    id TEXT PRIMARY KEY,
    direction TEXT NOT NULL CHECK (direction IN ('cash_in', 'cash_out')),
    name TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NULL REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_cash_transaction_categories_direction_name
    ON cash_transaction_categories(direction, lower(name));

CREATE TABLE manual_cash_transaction_sequences (
    transaction_date TEXT PRIMARY KEY,
    next_sequence INTEGER NOT NULL CHECK (next_sequence > 0)
);

CREATE TABLE manual_cash_transactions (
    id TEXT PRIMARY KEY,
    transaction_date TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('cash_in', 'cash_out')),
    category_id TEXT NOT NULL REFERENCES cash_transaction_categories(id),
    description TEXT NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    reference_no TEXT NOT NULL UNIQUE,
    note TEXT NOT NULL DEFAULT '',
    recorded_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_manual_cash_transactions_date
    ON manual_cash_transactions(transaction_date, created_at);
CREATE INDEX idx_manual_cash_transactions_direction_category
    ON manual_cash_transactions(direction, category_id);

CREATE TABLE cash_transaction_category_audits (
    id TEXT PRIMARY KEY,
    category_id TEXT NOT NULL REFERENCES cash_transaction_categories(id),
    actor_id TEXT NOT NULL REFERENCES users(id),
    action TEXT NOT NULL CHECK (action IN ('created', 'renamed', 'deactivated', 'reactivated')),
    old_name TEXT NOT NULL DEFAULT '',
    new_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_cash_transaction_category_audits_category
    ON cash_transaction_category_audits(category_id, created_at);

INSERT INTO cash_transaction_categories (id, direction, name)
VALUES
    ('cash-in-pendapatan-jasa', 'cash_in', 'Pendapatan Jasa'),
    ('cash-in-pengembalian-dana', 'cash_in', 'Pengembalian Dana'),
    ('cash-in-penerimaan-lainnya', 'cash_in', 'Penerimaan Lainnya'),
    ('cash-out-atk', 'cash_out', 'ATK'),
    ('cash-out-transportasi', 'cash_out', 'Transportasi'),
    ('cash-out-konsumsi', 'cash_out', 'Konsumsi'),
    ('cash-out-utilitas', 'cash_out', 'Utilitas'),
    ('cash-out-biaya-bank', 'cash_out', 'Biaya Bank'),
    ('cash-out-pemeliharaan', 'cash_out', 'Pemeliharaan'),
    ('cash-out-pengeluaran-lainnya', 'cash_out', 'Pengeluaran Lainnya');

CREATE FUNCTION protect_manual_cash_transactions_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'manual cash transactions are immutable';
END $$;
CREATE TRIGGER protect_manual_cash_transactions_immutable
BEFORE UPDATE OR DELETE ON manual_cash_transactions
FOR EACH ROW EXECUTE FUNCTION protect_manual_cash_transactions_immutable();

CREATE FUNCTION protect_cash_transaction_category_name() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM manual_cash_transactions WHERE category_id = OLD.id)
       AND NEW.name IS DISTINCT FROM OLD.name THEN
        RAISE EXCEPTION 'used cash transaction category names are immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_cash_transaction_category_name
BEFORE UPDATE OF name ON cash_transaction_categories
FOR EACH ROW EXECUTE FUNCTION protect_cash_transaction_category_name();

ALTER TABLE cash_transaction_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE manual_cash_transaction_sequences ENABLE ROW LEVEL SECURITY;
ALTER TABLE manual_cash_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cash_transaction_category_audits ENABLE ROW LEVEL SECURITY;

INSERT INTO schema_migrations (version, name)
VALUES (23, 'create_manual_cash_transactions');

COMMIT;
