SET app.current_actor = 'svc:migration';

DROP INDEX IF EXISTS idx_cases_analysis;
ALTER TABLE cases DROP COLUMN IF EXISTS reopened_count;
ALTER TABLE cases DROP COLUMN IF EXISTS sla_reminded_at;
ALTER TABLE cases DROP COLUMN IF EXISTS analysis_id;
