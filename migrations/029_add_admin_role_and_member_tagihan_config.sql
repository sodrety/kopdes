BEGIN;

ALTER TABLE officer_appointments DROP CONSTRAINT IF EXISTS officer_appointments_role_check;
ALTER TABLE officer_appointments ADD CONSTRAINT officer_appointments_role_check
    CHECK (role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'));

ALTER TABLE loan_request_approvals DROP CONSTRAINT IF EXISTS loan_request_approvals_officer_role_check;
ALTER TABLE loan_request_approvals ADD CONSTRAINT loan_request_approvals_officer_role_check
    CHECK (officer_role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'));

ALTER TABLE withdrawal_request_approvals DROP CONSTRAINT IF EXISTS withdrawal_request_approvals_officer_role_check;
ALTER TABLE withdrawal_request_approvals ADD CONSTRAINT withdrawal_request_approvals_officer_role_check
    CHECK (officer_role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'));

CREATE TABLE member_tagihan_configs (
    member_id TEXT PRIMARY KEY REFERENCES members(id),
    simpanan_wajib BIGINT NOT NULL DEFAULT 0 CHECK (simpanan_wajib >= 0),
    simpanan_manasuka BIGINT NOT NULL DEFAULT 0 CHECK (simpanan_manasuka >= 0),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO member_tagihan_configs (member_id, simpanan_wajib, simpanan_manasuka)
SELECT m.id,
    COALESCE((SELECT CASE WHEN sr.type='deposit' THEN sr.amount ELSE 0 END
        FROM saving_records sr
        WHERE sr.member_id=m.id AND sr.category='wajib'
        ORDER BY sr.record_date DESC,sr.created_at DESC,sr.id DESC LIMIT 1),0),
    COALESCE((SELECT CASE WHEN sr.type='deposit' THEN sr.amount ELSE 0 END
        FROM saving_records sr
        WHERE sr.member_id=m.id AND sr.category='sukarela'
        ORDER BY sr.record_date DESC,sr.created_at DESC,sr.id DESC LIMIT 1),0)
FROM members m
WHERE m.id IS NOT NULL
ON CONFLICT (member_id) DO NOTHING;

ALTER TABLE member_tagihan_configs ENABLE ROW LEVEL SECURITY;

INSERT INTO schema_migrations (version, name)
VALUES (29, 'add_admin_role_and_member_tagihan_config');

COMMIT;
