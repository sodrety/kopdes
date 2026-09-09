ALTER TABLE saving_records
    DROP CONSTRAINT IF EXISTS saving_records_category_check;

ALTER TABLE saving_records
    ADD CONSTRAINT saving_records_category_check
    CHECK (category IN ('pokok', 'wajib', 'sukarela', 'shu', 'khusus')) NOT VALID;
