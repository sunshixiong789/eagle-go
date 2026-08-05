-- +goose Up
-- +goose StatementBegin

-- OAuth2 客户端
CREATE TABLE oauth_client (
    client_id           varchar(64)  PRIMARY KEY,
    -- 公共客户端（SPA/App，走 PKCE）没有 secret，此处为空
    client_secret_hash  text         NOT NULL DEFAULT '',
    name                varchar(128) NOT NULL,
    -- authorization_code / refresh_token / client_credentials
    grant_types         text[]       NOT NULL DEFAULT '{}',
    redirect_uris       text[]       NOT NULL DEFAULT '{}',
    scopes              text[]       NOT NULL DEFAULT '{}',
    -- 公共客户端强制 PKCE
    is_public           boolean      NOT NULL DEFAULT false,
    access_ttl_seconds  int          NOT NULL DEFAULT 900,      -- 15min
    refresh_ttl_seconds int          NOT NULL DEFAULT 2592000,  -- 30d
    status              smallint     NOT NULL DEFAULT 1,
    created_at          timestamptz  NOT NULL DEFAULT now(),
    updated_at          timestamptz  NOT NULL DEFAULT now()
);

-- 授权码。短生命周期（默认 60s），但落库而非放 Redis：
-- 一是便于审计，二是用 UPDATE ... WHERE consumed_at IS NULL RETURNING
-- 在事务内原子地保证「一次性使用」，防重放。
CREATE TABLE oauth_auth_code (
    code                  text        PRIMARY KEY,
    client_id             varchar(64) NOT NULL REFERENCES oauth_client (client_id) ON DELETE CASCADE,
    user_id               bigint      NOT NULL,
    scopes                text[]      NOT NULL DEFAULT '{}',
    redirect_uri          text        NOT NULL,
    code_challenge        text        NOT NULL DEFAULT '',
    code_challenge_method varchar(16) NOT NULL DEFAULT '',
    nonce                 text        NOT NULL DEFAULT '',
    expires_at            timestamptz NOT NULL,
    consumed_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_oauth_auth_code_expires ON oauth_auth_code (expires_at);

-- token 签名密钥（支持轮转，公钥经 JWKS 端点暴露）
--
-- 安全提示：private_key_pem 是明文落库。生产环境应改为
-- KMS/Vault 托管，或至少做信封加密后再写入本字段。
-- 本表已按此前提设计（algorithm/kid 分离），替换存储实现不影响调用方。
CREATE TABLE signing_key (
    kid             varchar(64) PRIMARY KEY,
    algorithm       varchar(16) NOT NULL DEFAULT 'RS256',
    private_key_pem text        NOT NULL,
    public_key_pem  text        NOT NULL,
    -- 只有一把 active 密钥用于签发；非 active 的仍保留在 JWKS 里供验签，直到过期
    active          boolean     NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz
);
CREATE UNIQUE INDEX uk_signing_key_active ON signing_key (active) WHERE active;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS signing_key;
DROP TABLE IF EXISTS oauth_auth_code;
DROP TABLE IF EXISTS oauth_client;
-- +goose StatementEnd
