UPDATE roles SET permissions = '["application.read", "application.write", "websocket.connect", "metric.read", "organization.read", "organization.write", "token.read", "token.write"]'
WHERE organisation_id IS NULL AND name IN ('admin', 'operator');

UPDATE roles SET permissions = '["application.read", "metric.read", "organization.read", "token.read"]'
WHERE organisation_id IS NULL AND name = 'viewer';
