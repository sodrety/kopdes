BEGIN;

ALTER TABLE cash_transaction_categories ADD COLUMN category_key TEXT;
ALTER TABLE cash_transaction_categories ADD COLUMN account_code TEXT;
ALTER TABLE cash_transaction_categories ADD COLUMN normal_balance TEXT CHECK (normal_balance IS NULL OR normal_balance IN ('D', 'C'));
ALTER TABLE cash_transaction_categories ADD COLUMN parent_id TEXT NULL REFERENCES cash_transaction_categories(id);
ALTER TABLE cash_transaction_categories ADD COLUMN is_group BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE cash_transaction_categories
SET category_key = id
WHERE category_key IS NULL OR category_key = '';

CREATE UNIQUE INDEX idx_cash_transaction_categories_category_key
    ON cash_transaction_categories(category_key);
CREATE INDEX idx_cash_transaction_categories_parent
    ON cash_transaction_categories(parent_id);

INSERT INTO schema_migrations (version, name)
VALUES (26, 'add_cash_transaction_category_tree');

COMMIT;
