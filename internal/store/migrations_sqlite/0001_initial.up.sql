CREATE TABLE organisations (
    id          text PRIMARY KEY,
    name        text NOT NULL UNIQUE
                CHECK (length(name) >= 3 AND length(name) <= 30),
    created_at  text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE applications (
    id               text PRIMARY KEY,
    organisation_id  text NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    name             text NOT NULL
                     CHECK (length(name) >= 3 AND length(name) <= 255),
    settings         text NOT NULL DEFAULT '{}',
    created_at       text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (organisation_id, name)
);

CREATE TABLE instances (
    id                 text PRIMARY KEY,
    advertise_address  text NOT NULL,
    port               integer NOT NULL,
    started_at         text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    heartbeat_at       text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE executors (
    id                   text PRIMARY KEY,
    application_id       text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    executor_id          text NOT NULL,
    application_version  text NOT NULL DEFAULT '',
    hostname             text NOT NULL DEFAULT '',
    metadata             text NOT NULL DEFAULT '{}',
    status               text NOT NULL DEFAULT 'connected'
                         CHECK (status IN ('connected', 'disconnected', 'dead')),
    owner_instance_id    text REFERENCES instances(id) ON DELETE SET NULL,
    lease_expires_at     text,
    connected_at         text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at         text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    disconnected_at      text,
    UNIQUE (application_id, executor_id)
);

CREATE INDEX executors_status_lease_idx
    ON executors (status, lease_expires_at);
CREATE INDEX executors_status_disconnected_idx
    ON executors (status, disconnected_at);
CREATE INDEX executors_owner_idx
    ON executors (owner_instance_id);

CREATE TABLE api_keys (
    id                text PRIMARY KEY,
    organisation_id   text NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    name              text NOT NULL,
    lookup            text NOT NULL UNIQUE,
    key_hash          blob NOT NULL,
    application_names text NOT NULL DEFAULT '[]',
    permissions       text NOT NULL DEFAULT '[]',
    created_at        text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at      text,
    revoked_at        text
);
