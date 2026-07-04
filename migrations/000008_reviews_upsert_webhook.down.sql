SET app.current_actor = 'svc:migration';

UPDATE sources
SET config = config - 'webhook_secret'
WHERE name = 'webhook_generic';

ALTER TABLE reviews DROP CONSTRAINT reviews_source_external_key;
