CREATE TABLE users (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subject     text NOT NULL UNIQUE,
    username    text NOT NULL UNIQUE CHECK (username ~ '^[a-zA-Z0-9_\-\.]{1,64}$'),
    email       text NOT NULL DEFAULT '',
    is_admin    boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id  uuid REFERENCES organisations(id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (name ~ '^[a-zA-Z0-9_]{3,30}$'),
    permissions      text[] NOT NULL DEFAULT '{}',
    is_global        boolean NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organisation_id, name)
);

CREATE UNIQUE INDEX roles_global_name_idx ON roles (name) WHERE organisation_id IS NULL;

INSERT INTO roles (organisation_id, name, permissions, is_global)
VALUES
    (NULL, 'admin', ARRAY['application.read', 'application.write', 'websocket.connect'], true),
    (NULL, 'operator', ARRAY['application.read', 'application.write', 'websocket.connect'], true),
    (NULL, 'viewer', ARRAY['application.read'], true);

CREATE TABLE organisation_members (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id  uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_name        text NOT NULL DEFAULT 'viewer',
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organisation_id, user_id)
);

CREATE TABLE domain_claims (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id  uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    domain           text NOT NULL UNIQUE,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id  uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    user_id          uuid REFERENCES users(id) ON DELETE SET NULL,
    username         text NOT NULL DEFAULT '',
    action           text NOT NULL,
    details          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_org_idx ON audit_logs (organisation_id, created_at DESC);
