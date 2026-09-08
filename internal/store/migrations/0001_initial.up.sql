CREATE TABLE organisations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE
                CHECK (name ~ '^[a-z0-9_]{3,30}$'),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE applications (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id  uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    name             text NOT NULL
                     CHECK (name ~ '^[a-z0-9\-_]{3,255}$'),
    settings         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organisation_id, name)
);

CREATE TABLE instances (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    advertise_address  text NOT NULL,
    port               integer NOT NULL,
    started_at         timestamptz NOT NULL DEFAULT now(),
    heartbeat_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE executor_status AS ENUM ('connected', 'disconnected', 'dead');

CREATE TABLE executors (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id       uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    executor_id          text NOT NULL,
    application_version  text NOT NULL DEFAULT '',
    hostname             text NOT NULL DEFAULT '',
    metadata             jsonb NOT NULL DEFAULT '{}'::jsonb,
    status               executor_status NOT NULL DEFAULT 'connected',
    owner_instance_id    uuid REFERENCES instances(id) ON DELETE SET NULL,
    lease_expires_at     timestamptz,
    connected_at         timestamptz NOT NULL DEFAULT now(),
    last_seen_at         timestamptz NOT NULL DEFAULT now(),
    disconnected_at      timestamptz,
    UNIQUE (application_id, executor_id)
);

CREATE INDEX executors_status_lease_idx
    ON executors (status, lease_expires_at);
CREATE INDEX executors_status_disconnected_idx
    ON executors (status, disconnected_at);
CREATE INDEX executors_owner_idx
    ON executors (owner_instance_id);

CREATE TABLE api_keys (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id  uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    name             text NOT NULL,
    lookup           text NOT NULL UNIQUE,
    key_hash         bytea NOT NULL,
    application_names text[] NOT NULL DEFAULT '{}',
    permissions      text[] NOT NULL DEFAULT '{}',
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_used_at     timestamptz,
    revoked_at       timestamptz
);
