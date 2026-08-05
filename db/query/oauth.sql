-- ── OAuth2 客户端 ──────────────────────────────────────

-- name: GetOAuthClient :one
SELECT * FROM oauth_client WHERE client_id = @client_id AND status = 1;

-- name: CreateOAuthClient :one
INSERT INTO oauth_client (
    client_id, client_secret_hash, name, grant_types, redirect_uris,
    scopes, is_public, access_ttl_seconds, refresh_ttl_seconds
) VALUES (
    @client_id, @client_secret_hash, @name, @grant_types, @redirect_uris,
    @scopes, @is_public, @access_ttl_seconds, @refresh_ttl_seconds
)
RETURNING *;

-- name: ListOAuthClients :many
SELECT * FROM oauth_client ORDER BY created_at DESC;

-- name: DeleteOAuthClient :execrows
DELETE FROM oauth_client WHERE client_id = @client_id;

-- ── 授权码 ─────────────────────────────────────────────

-- name: CreateAuthCode :exec
INSERT INTO oauth_auth_code (
    code, client_id, user_id, scopes, redirect_uri,
    code_challenge, code_challenge_method, nonce, expires_at
) VALUES (
    @code, @client_id, @user_id, @scopes, @redirect_uri,
    @code_challenge, @code_challenge_method, @nonce, @expires_at
);

-- name: ConsumeAuthCode :one
-- 单次原子操作完成「取出 + 标记已用」。并发重放时第二次拿不到行，
-- 从而在数据库层面保证授权码一次性，不依赖应用层加锁。
UPDATE oauth_auth_code
SET consumed_at = now()
WHERE code = @code
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: DeleteExpiredAuthCodes :execrows
DELETE FROM oauth_auth_code WHERE expires_at < now() - interval '1 hour';

-- ── 签名密钥 ───────────────────────────────────────────

-- name: GetActiveSigningKey :one
SELECT * FROM signing_key WHERE active;

-- name: ListValidSigningKeys :many
-- JWKS 端点用：active 的负责签发，未过期的旧密钥仍需保留以验证存量 token
SELECT * FROM signing_key
WHERE expires_at IS NULL OR expires_at > now()
ORDER BY created_at DESC;

-- name: GetSigningKeyByKID :one
SELECT * FROM signing_key WHERE kid = @kid;

-- name: DeactivateAllSigningKeys :exec
UPDATE signing_key SET active = false WHERE active;

-- name: CreateSigningKey :one
INSERT INTO signing_key (kid, algorithm, private_key_pem, public_key_pem, active, expires_at)
VALUES (@kid, @algorithm, @private_key_pem, @public_key_pem, @active, @expires_at)
RETURNING *;

-- name: DeleteExpiredSigningKeys :execrows
DELETE FROM signing_key WHERE expires_at IS NOT NULL AND expires_at < now();
