-- +goose Up
-- +goose StatementBegin

CREATE TABLE stored_file (
    id              varchar(36)  PRIMARY KEY,
    owner_subject   varchar(128) NOT NULL,
    name            varchar(255) NOT NULL,
    storage_key     varchar(255) NOT NULL UNIQUE,
    content_type    varchar(128) NOT NULL DEFAULT 'application/octet-stream',
    size            bigint       NOT NULL CHECK (size >= 0),
    sha256          varchar(64)  NOT NULL,
    created_at      timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_stored_file_owner_created ON stored_file (owner_subject, created_at DESC);

CREATE TABLE notification (
    id                bigserial    PRIMARY KEY,
    recipient_subject varchar(128) NOT NULL,
    sender_subject    varchar(128) NOT NULL DEFAULT '',
    title             varchar(128) NOT NULL,
    content           varchar(4096) NOT NULL,
    read_at           timestamptz,
    created_at        timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_notification_recipient_created ON notification (recipient_subject, created_at DESC);
CREATE INDEX idx_notification_recipient_unread ON notification (recipient_subject, id) WHERE read_at IS NULL;

INSERT INTO permission_definition (code, service, resource, action, status, source) VALUES
    ('system:file:add', 'system', 'file', 'add', 1, 'migration'),
    ('system:file:remove', 'system', 'file', 'remove', 1, 'migration'),
    ('system:notification:send', 'system', 'notification', 'send', 1, 'migration')
ON CONFLICT (code) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM permission_definition
WHERE code IN ('system:file:add', 'system:file:remove', 'system:notification:send');
DROP TABLE IF EXISTS notification;
DROP TABLE IF EXISTS stored_file;

-- +goose StatementEnd
