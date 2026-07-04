-- =============================================================
-- 000010: M5 Routing 前置
--   cases.analysis_id：案件當前依據的分析（冪等指標——同一份分析不重複路由，
--                      也是 analyzed-未建案對帳的判定鍵）
--   reopen 語意（M3/M4 審查必解）：保留 review_id UNIQUE，
--   一則評論一個案件執行緒；已結案遇新分析 → reopen（reopened_count 留痕）
--   sla_reminded_at：SLA 逾期提醒只發一次（PoC 只提醒不升級）
-- =============================================================

SET app.current_actor = 'svc:migration';

ALTER TABLE cases ADD COLUMN analysis_id uuid REFERENCES analysis_results (id);
ALTER TABLE cases ADD COLUMN sla_reminded_at timestamptz;
ALTER TABLE cases ADD COLUMN reopened_count integer NOT NULL DEFAULT 0;

-- 對帳掃描：現行分析未被案件認領（無 case 或指標不符）
CREATE INDEX idx_cases_analysis ON cases (analysis_id);
