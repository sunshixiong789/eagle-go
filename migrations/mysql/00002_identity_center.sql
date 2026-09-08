-- +goose Up
CREATE TABLE user_account (
    subject      varchar(32)   NOT NULL,
    display_name varchar(128)  NOT NULL DEFAULT '',
    avatar_url   varchar(2048) NOT NULL DEFAULT '',
    status       int           NOT NULL DEFAULT 1,
    created_at   datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at   datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (subject),
    CONSTRAINT ck_user_account_status CHECK (status IN (0, 1))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;

CREATE TABLE user_identity (
    id               bigint        NOT NULL AUTO_INCREMENT,
    account_subject  varchar(32)   NOT NULL,
    provider         varchar(32)   NOT NULL,
    provider_subject varchar(255)  NOT NULL,
    email            varchar(320)  NOT NULL DEFAULT '',
    email_verified   boolean       NOT NULL DEFAULT false,
    last_login_at    datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_at       datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at       datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_identity_provider_subject (provider, provider_subject),
    CONSTRAINT fk_user_identity_account FOREIGN KEY (account_subject) REFERENCES user_account(subject) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_user_identity_account ON user_identity (account_subject);

CREATE TABLE user_role_binding (
    id              bigint       NOT NULL AUTO_INCREMENT,
    account_subject varchar(32)  NOT NULL,
    audience        varchar(255) NOT NULL,
    role            varchar(64)  NOT NULL,
    created_at      datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_role_binding_account_audience_role (account_subject, audience, role),
    CONSTRAINT fk_user_role_binding_account FOREIGN KEY (account_subject) REFERENCES user_account(subject) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_user_role_binding_account_audience ON user_role_binding (account_subject, audience);

INSERT INTO user_account (subject, display_name, avatar_url, created_at, updated_at)
SELECT MD5(subject), display_name, avatar_url, created_at, updated_at
FROM social_identity;

INSERT INTO user_identity (
    id, account_subject, provider, provider_subject, email, email_verified,
    last_login_at, created_at, updated_at
)
SELECT id, MD5(subject), provider, provider_subject, email, email_verified,
       last_login_at, created_at, updated_at
FROM social_identity;

INSERT INTO user_role_binding (account_subject, audience, role, created_at, updated_at)
SELECT MD5(subject), 'eagle-api', role, created_at, updated_at
FROM social_identity;

ALTER TABLE auth_session DROP FOREIGN KEY fk_auth_session_identity;
ALTER TABLE auth_session ADD COLUMN audience varchar(255) NOT NULL DEFAULT 'eagle-api' AFTER identity_id;
ALTER TABLE auth_session
    ADD CONSTRAINT fk_auth_session_identity FOREIGN KEY (identity_id) REFERENCES user_identity(id) ON DELETE CASCADE;

DROP TABLE social_identity;

-- +goose Down
CREATE TABLE social_identity (
    id               bigint        NOT NULL AUTO_INCREMENT,
    subject          varchar(384)  NOT NULL,
    provider         varchar(16)   NOT NULL,
    provider_subject varchar(255)  NOT NULL,
    email            varchar(320)  NOT NULL DEFAULT '',
    email_verified   boolean       NOT NULL DEFAULT false,
    display_name     varchar(128)  NOT NULL DEFAULT '',
    avatar_url       varchar(2048) NOT NULL DEFAULT '',
    role             varchar(32)   NOT NULL DEFAULT 'user',
    last_login_at    datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_at       datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at       datetime(6)   NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_social_identity_subject (subject),
    UNIQUE KEY uk_social_identity_provider_subject (provider, provider_subject),
    CONSTRAINT ck_social_identity_provider CHECK (provider IN ('google', 'apple')),
    CONSTRAINT ck_social_identity_role CHECK (role IN ('user', 'admin'))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;

INSERT INTO social_identity (
    id, subject, provider, provider_subject, email, email_verified,
    display_name, avatar_url, role, last_login_at, created_at, updated_at
)
SELECT i.id,
       CONCAT(i.provider, ':', i.provider_subject),
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

ALTER TABLE auth_session DROP FOREIGN KEY fk_auth_session_identity;
ALTER TABLE auth_session DROP COLUMN audience;
ALTER TABLE auth_session
    ADD CONSTRAINT fk_auth_session_identity FOREIGN KEY (identity_id) REFERENCES social_identity(id) ON DELETE CASCADE;

DROP TABLE user_role_binding;
DROP TABLE user_identity;
DROP TABLE user_account;
