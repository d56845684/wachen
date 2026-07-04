SET app.current_actor = 'svc:migration';

UPDATE users SET password_hash = NULL WHERE email = 'admin@example.com';
