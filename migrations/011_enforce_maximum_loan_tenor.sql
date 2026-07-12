-- PostgreSQL deployment migration. NOT VALID preserves any legacy rows above
-- the new limit while enforcing 1..120 months for all future writes.
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_duration_max
CHECK (duration_months BETWEEN 1 AND 120) NOT VALID;

ALTER TABLE loans ADD CONSTRAINT loans_duration_max
CHECK (duration_months BETWEEN 1 AND 120) NOT VALID;
