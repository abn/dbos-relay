CREATE TABLE users (
    id          text PRIMARY KEY,
    subject     text NOT NULL UNIQUE,
    username    text NOT NULL UNIQUE CHECK (length(username) >= 1 AND length(username) <= 64),
    email       text NOT NULL DEFAULT '',
    is_admin    integer NOT NULL DEFAULT 0,
    created_at  text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE roles (
    id               text PRIMARY KEY,
    organisation_id  text REFERENCES organisations(id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (length(name) >= 3 AND length(name) <= 30),
    permissions      text NOT NULL DEFAULT '[]',
    is_global        integer NOT NULL DEFAULT 0,
    created_at       text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (organisation_id, name)
);

CREATE UNIQUE INDEX roles_global_name_idx ON roles (name) WHERE organisation_id IS NULL;

INSERT INTO roles (id, organisation_id, name, permissions, is_global, created_at)
VALUES
    ('00000000-0000-0000-0000-000000000001', NULL, 'admin', '["application.read", "application.write", "websocket.connect"]', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('00000000-0000-0000-0000-000000000002', NULL, 'operator', '["application.read", "application.write", "websocket.connect"]', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('00000000-0000-0000-0000-000000000003', NULL, 'viewer', '["application.read"]', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE organisation_members (
    id               text PRIMARY KEY,
    organisation_id  text NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    user_id          text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_name        text NOT NULL DEFAULT 'viewer',
    created_at       text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (organisation_id, user_id)
);

CREATE TABLE domain_claims (
    id               text PRIMARY KEY,
    organisation_id  text NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    domain           text NOT NULL UNIQUE,
    created_at       text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE audit_logs (
    id               text PRIMARY KEY,
    organisation_id  text NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    user_id          text REFERENCES users(id) ON DELETE SET NULL,
    username         text NOT NULL DEFAULT '',
    action           text NOT NULL,
    details          text NOT NULL DEFAULT '{}',
    created_at       text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX audit_logs_org_idx ON audit_logs (organisation_id, created_at DESC);
