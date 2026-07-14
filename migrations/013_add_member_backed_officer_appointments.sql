-- PostgreSQL mirror of runtime migration 13 in internal/app/migrations.go.
-- Before applying, populate legacy_officer_member_mappings for every legacy
-- users.role <> 'member' row. The application can prepare these rows from the
-- LEGACY_OFFICER_MEMBER_MAPPINGS environment variable.
BEGIN;
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS legacy_officer_member_mappings (
    legacy_user_id TEXT PRIMARY KEY REFERENCES users(id),
    member_id TEXT NOT NULL UNIQUE REFERENCES members(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE users ADD COLUMN historical_identity BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE officer_appointments (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL UNIQUE REFERENCES members(id),
    role TEXT NOT NULL CHECK (role IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_officer_appointments_role_active ON officer_appointments(role, active);

ALTER TABLE loan_request_approvals ADD COLUMN officer_member_id TEXT NOT NULL DEFAULT '';
ALTER TABLE loan_request_approvals ADD COLUMN officer_member_no TEXT NOT NULL DEFAULT '';
ALTER TABLE withdrawal_request_approvals ADD COLUMN officer_member_id TEXT NOT NULL DEFAULT '';
ALTER TABLE withdrawal_request_approvals ADD COLUMN officer_member_no TEXT NOT NULL DEFAULT '';
ALTER TABLE officer_audit_events ADD COLUMN actor_member_id TEXT NOT NULL DEFAULT '';
ALTER TABLE officer_audit_events ADD COLUMN actor_member_no TEXT NOT NULL DEFAULT '';
ALTER TABLE officer_audit_events ADD COLUMN target_member_id TEXT NOT NULL DEFAULT '';
ALTER TABLE officer_audit_events ADD COLUMN target_member_no TEXT NOT NULL DEFAULT '';
ALTER TABLE officer_audit_events ADD COLUMN target_appointment_id TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN audience TEXT NOT NULL DEFAULT 'officer' CHECK (audience IN ('member', 'officer'));
UPDATE notifications SET audience = CASE WHEN link LIKE '/member/%' THEN 'member' ELSE 'officer' END;
CREATE INDEX idx_notifications_user_audience_state ON notifications(user_id, audience, resolved_at, is_read, created_at);

DO $$
DECLARE
    legacy RECORD;
    mapped_member RECORD;
    mapped_member_id TEXT;
    canonical_user_id TEXT;
    appointment_id TEXT;
BEGIN
    FOR legacy IN
        SELECT id, role
        FROM users
        WHERE role <> 'member' AND historical_identity = FALSE
        ORDER BY id
    LOOP
        SELECT member_id INTO mapped_member_id
        FROM legacy_officer_member_mappings
        WHERE legacy_user_id = legacy.id;
        IF mapped_member_id IS NULL THEN
            RAISE EXCEPTION 'legacy Officer user % requires an explicit Member mapping', legacy.id;
        END IF;

        SELECT id, member_no, full_name, status INTO mapped_member
        FROM members
        WHERE id = mapped_member_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'mapped Member % does not exist', mapped_member_id;
        END IF;
        IF mapped_member.status <> 'active' THEN
            RAISE EXCEPTION 'mapped Member % must be active', mapped_member_id;
        END IF;

        canonical_user_id := NULL;
        SELECT id INTO canonical_user_id
        FROM users
        WHERE member_id = mapped_member_id
          AND id <> legacy.id
          AND historical_identity = FALSE
        ORDER BY created_at, id
        LIMIT 1;

        IF canonical_user_id IS NOT NULL THEN
            UPDATE users
            SET role = 'member', member_id = NULL, full_name = mapped_member.full_name,
                active = FALSE, historical_identity = TRUE,
                password_hash = '!historical!', updated_at = CURRENT_TIMESTAMP
            WHERE id = legacy.id;
            UPDATE notifications SET user_id = canonical_user_id WHERE user_id = legacy.id;
        ELSE
            canonical_user_id := legacy.id;
            UPDATE users
            SET role = 'member', member_id = mapped_member_id,
                full_name = mapped_member.full_name,
                historical_identity = FALSE, updated_at = CURRENT_TIMESTAMP
            WHERE id = legacy.id;
        END IF;

        appointment_id := 'appointment-' || legacy.id;
        INSERT INTO officer_appointments (id, member_id, role, active)
        VALUES (appointment_id, mapped_member_id, legacy.role, TRUE);

        UPDATE loan_request_approvals
        SET officer_member_id = mapped_member_id,
            officer_member_no = mapped_member.member_no,
            officer_name = CASE WHEN officer_name = '' THEN mapped_member.full_name ELSE officer_name END
        WHERE officer_id = legacy.id;
        UPDATE withdrawal_request_approvals
        SET officer_member_id = mapped_member_id,
            officer_member_no = mapped_member.member_no,
            officer_name = CASE WHEN officer_name = '' THEN mapped_member.full_name ELSE officer_name END
        WHERE officer_id = legacy.id;
        UPDATE officer_audit_events
        SET actor_member_id = mapped_member_id,
            actor_member_no = mapped_member.member_no,
            actor_name = CASE WHEN actor_name = '' THEN mapped_member.full_name ELSE actor_name END
        WHERE actor_id = legacy.id;
        UPDATE officer_audit_events
        SET target_member_id = mapped_member_id,
            target_member_no = mapped_member.member_no,
            target_name = CASE WHEN target_name = '' THEN mapped_member.full_name ELSE target_name END,
            target_appointment_id = appointment_id
        WHERE target_id = legacy.id;
    END LOOP;
END $$;

CREATE UNIQUE INDEX idx_users_one_current_per_member
ON users(member_id)
WHERE member_id IS NOT NULL AND historical_identity = FALSE;

CREATE FUNCTION protect_last_ketua_utama_member_deactivation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'active' AND NEW.status <> 'active'
       AND EXISTS (SELECT 1 FROM officer_appointments WHERE member_id = OLD.id AND role = 'ketua_utama' AND active = TRUE)
       AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id = oa.member_id WHERE oa.role = 'ketua_utama' AND oa.active = TRUE AND m.status = 'active') <= 1
    THEN
        RAISE EXCEPTION 'at least one active Ketua Utama is required';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_last_ketua_utama_member_deactivation
BEFORE UPDATE OF status ON members
FOR EACH ROW EXECUTE FUNCTION protect_last_ketua_utama_member_deactivation();

CREATE FUNCTION suspend_officer_on_member_deactivation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'active' AND NEW.status <> 'active' THEN
        UPDATE officer_appointments
        SET active = FALSE, updated_at = CURRENT_TIMESTAMP
        WHERE member_id = NEW.id AND active = TRUE;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER suspend_officer_on_member_deactivation
AFTER UPDATE OF status ON members
FOR EACH ROW EXECUTE FUNCTION suspend_officer_on_member_deactivation();

CREATE FUNCTION protect_last_ketua_utama_appointment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.role = 'ketua_utama' AND OLD.active = TRUE
       AND (NEW.role <> 'ketua_utama' OR NEW.active = FALSE)
       AND EXISTS (SELECT 1 FROM members WHERE id = OLD.member_id AND status = 'active')
       AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id = oa.member_id WHERE oa.role = 'ketua_utama' AND oa.active = TRUE AND m.status = 'active') <= 1
    THEN
        RAISE EXCEPTION 'at least one active Ketua Utama is required';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER protect_last_ketua_utama_appointment
BEFORE UPDATE OF role, active ON officer_appointments
FOR EACH ROW EXECUTE FUNCTION protect_last_ketua_utama_appointment();

REVOKE EXECUTE ON FUNCTION protect_last_ketua_utama_member_deactivation() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION suspend_officer_on_member_deactivation() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION protect_last_ketua_utama_appointment() FROM PUBLIC;

-- Server-side access only. No Data API policies are intentionally added.
ALTER TABLE legacy_officer_member_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE officer_appointments ENABLE ROW LEVEL SECURITY;

INSERT INTO schema_migrations (version, name)
VALUES (13, 'add_member_backed_officer_appointments');

COMMIT;
