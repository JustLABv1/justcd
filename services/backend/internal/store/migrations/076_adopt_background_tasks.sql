ALTER TABLE background_tasks DROP CONSTRAINT IF EXISTS background_tasks_kind_check;
ALTER TABLE background_tasks ADD CONSTRAINT background_tasks_kind_check CHECK (kind IN ('plan', 'approval', 'adopt'));
