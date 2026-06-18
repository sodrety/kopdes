ALTER TABLE saving_records
ADD COLUMN category TEXT NOT NULL DEFAULT 'sukarela' CHECK (category IN ('pokok', 'wajib', 'sukarela'));
