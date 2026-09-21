UPDATE roles SET permissions = ARRAY['application.read', 'application.write', 'websocket.connect']
WHERE organisation_id IS NULL AND name IN ('admin', 'operator');

UPDATE roles SET permissions = ARRAY['application.read']
WHERE organisation_id IS NULL AND name = 'viewer';
