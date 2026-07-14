ALTER FUNCTION protect_last_ketua_utama_member_deactivation() SET search_path = public, pg_temp;
ALTER FUNCTION suspend_officer_on_member_deactivation() SET search_path = public, pg_temp;
ALTER FUNCTION protect_last_ketua_utama_appointment() SET search_path = public, pg_temp;

INSERT INTO schema_migrations (version, name)
VALUES (14, 'lock_officer_trigger_function_search_path');
