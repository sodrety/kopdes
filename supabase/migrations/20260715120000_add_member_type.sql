ALTER TABLE members
ADD COLUMN member_type TEXT NOT NULL DEFAULT 'employee'
CHECK (member_type IN ('daily_worker', 'employee', 'self_employed'));
