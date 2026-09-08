-- +goose Up
-- +goose StatementBegin
-- 账号与登录凭证分离：一个 Eagle 账号可以显式绑定多个第三方身份，
-- 角色按 token audience 隔离，为后续独立认证中心保留稳定的数据边界。
CREATE TABLE user_account (
    subject      varchar(32)   PRIMARY KEY,
    display_name varchar(128)  NOT NULL DEFAULT '',
    avatar_url   varchar(2048) NOT NULL DEFAULT '',
    status       int           NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at   timestamptz   NOT NULL DEFAULT now(),
    updated_at   timestamptz   NOT NULL DEFAULT now()
);

CREATE TABLE user_identity (
    id               bigserial     PRIMARY KEY,
    account_subject  varchar(32)   NOT NULL REFERENCES user_account(subject) ON DELETE CASCADE,
    provider         varchar(32)   NOT NULL,
    provider_subject varchar(255)  NOT NULL,
    email            varchar(320)  NOT NULL DEFAULT '',
    email_verified   boolean       NOT NULL DEFAULT false,
    last_login_at    timestamptz   NOT NULL DEFAULT now(),
    created_at       timestamptz   NOT NULL DEFAULT now(),
    updated_at       timestamptz   NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_subject)
);
CREATE INDEX idx_user_identity_account ON user_identity (account_subject);

CREATE TABLE user_role_binding (
    id              bigserial    PRIMARY KEY,
    account_subject varchar(32)  NOT NULL REFERENCES user_account(subject) ON DELETE CASCADE,
    audience        varchar(255) NOT NULL,
    role            varchar(64)  NOT NULL,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    UNIQUE (account_subject, audience, role)
);
CREATE INDEX idx_user_role_binding_account_audience ON user_role_binding (account_subject, audience);

-- 旧 subject 带第三方 provider 信息；迁移后换成不透明的 32 字符账号主体。
INSERT INTO user_account (subject, display_name, avatar_url, created_at, updated_at)
SELECT md5(subject), display_name, avatar_url, created_at, updated_at
FROM social_identity;

INSERT INTO user_identity (
    id, account_subject, provider, provider_subject, email, email_verified,
    last_login_at, created_at, updated_at
)
SELECT id, md5(subject), provider, provider_subject, email, email_verified,
       last_login_at, created_at, updated_at
FROM social_identity;

INSERT INTO user_role_binding (account_subject, audience, role, created_at, updated_at)
SELECT md5(subject), 'eagle-api', role, created_at, updated_at
FROM social_identity;

SELECT setval(
    pg_get_serial_sequence('user_identity', 'id'),
    COALESCE((SELECT max(id) FROM user_identity), 1),
    EXISTS (SELECT 1 FROM user_identity)
);

ALTER TABLE auth_session DROP CONSTRAINT auth_session_identity_id_fkey;
ALTER TABLE auth_session ADD COLUMN audience varchar(255) NOT NULL DEFAULT 'eagle-api';
ALTER TABLE auth_session
    ADD CONSTRAINT auth_session_identity_id_fkey
    FOREIGN KEY (identity_id) REFERENCES user_identity(id) ON DELETE CASCADE;

DROP TABLE social_identity;
-- +goose StatementEnd

-- +goose Down
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

INSERT INTO social_identity (
    id, subject, provider, provider_subject, email, email_verified,
    display_name, avatar_url, role, last_login_at, created_at, updated_at
)
SELECT i.id,
       i.provider || ':' || i.provider_subject,
       i.provider,
       i.provider_subject,
       i.email,
       i.email_verified,
       a.display_name,
       a.avatar_url,
       COALESCE((
           SELECT CASE WHEN r.role IN ('user', 'admin') THEN r.role ELSE 'user' END
           FROM user_role_binding r
           WHERE r.account_subject = i.account_subject AND r.audience = 'eagle-api'
           ORDER BY r.id LIMIT 1
       ), 'user'),
       i.last_login_at,
       i.created_at,
       i.updated_at
FROM user_identity i
JOIN user_account a ON a.subject = i.account_subject;

SELECT setval(
    pg_get_serial_sequence('social_identity', 'id'),
    COALESCE((SELECT max(id) FROM social_identity), 1),
    EXISTS (SELECT 1 FROM social_identity)
);

ALTER TABLE auth_session DROP CONSTRAINT auth_session_identity_id_fkey;
ALTER TABLE auth_session DROP COLUMN audience;
ALTER TABLE auth_session
    ADD CONSTRAINT auth_session_identity_id_fkey
    FOREIGN KEY (identity_id) REFERENCES social_identity(id) ON DELETE CASCADE;

DROP TABLE user_role_binding;
DROP TABLE user_identity;
DROP TABLE user_account;
-- +goose StatementEnd
