-- +goose Up
-- +goose StatementBegin

CREATE TABLE social_identity (
    id               bigserial     PRIMARY KEY,
    subject          varchar(384)  NOT NULL UNIQUE,
    provider         varchar(16)   NOT NULL CHECK (provider IN ('google', 'apple')),
    provider_subject varchar(255)  NOT NULL,
    email            varchar(320)  NOT NULL DEFAULT '',
    email_verified   boolean       NOT NULL DEFAULT false,
    display_name     varchar(128)  NOT NULL DEFAULT '',
    avatar_url       varchar(2048) NOT NULL DEFAULT '',
    role             varchar(32)   NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    last_login_at    timestamptz   NOT NULL DEFAULT now(),
    created_at       timestamptz   NOT NULL DEFAULT now(),
    updated_at       timestamptz   NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_subject)
);

CREATE TABLE auth_session (
    id                 varchar(32) PRIMARY KEY,
    identity_id        bigint      NOT NULL REFERENCES social_identity(id) ON DELETE CASCADE,
    refresh_token_hash varchar(64) NOT NULL UNIQUE,
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_auth_session_identity ON auth_session (identity_id);
CREATE INDEX idx_auth_session_expires ON auth_session (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS auth_session;
DROP TABLE IF EXISTS social_identity;
-- +goose StatementEnd
