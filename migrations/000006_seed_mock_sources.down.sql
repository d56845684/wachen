SET app.current_actor = 'svc:migration';

-- crawl_jobs / raw_reviews 可能已引用這些來源（且 raw_reviews 為 append-only），
-- 因此 down 採軟刪除而非硬刪除
UPDATE sources
SET enabled = false, deleted_at = now(), deleted_by = current_actor()
WHERE name IN ('google_review_mock_a', 'google_review_mock_b');
