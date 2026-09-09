alter table public.saving_records
    drop constraint if exists saving_records_category_check;

alter table public.saving_records
    add constraint saving_records_category_check
    check (category in ('pokok', 'wajib', 'sukarela', 'shu', 'khusus')) not valid;
