-- +goose Up
-- 增量增加角色初始化标记、并发状态和审计；不改变旧应用使用的表结构。
CREATE TABLE account_role_state (
    audience varchar(255) PRIMARY KEY,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    admin_initialized boolean NOT NULL DEFAULT false,
    updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE user_audience (
    id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    account_subject varchar(32) NOT NULL,
    audience varchar(255) NOT NULL,
    created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_user_audience_account FOREIGN KEY (account_subject) REFERENCES user_account(subject) ON DELETE CASCADE,
    UNIQUE (account_subject, audience)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE account_role_audit (
    id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    audience varchar(255) NOT NULL,
    revision bigint NOT NULL,
    account_subject varchar(32) NOT NULL,
    actor_subject varchar(128) NOT NULL,
    action varchar(32) NOT NULL,
    `before` json NOT NULL,
    `after` json NOT NULL,
    created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE (audience, revision)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_account_role_audit_target ON account_role_audit(account_subject, created_at);

INSERT INTO user_audience (account_subject, audience)
SELECT account_subject, audience FROM user_role_binding
UNION
SELECT i.account_subject, s.audience FROM auth_session s JOIN user_identity i ON i.id = s.identity_id;
INSERT INTO account_role_state (audience, admin_initialized)
SELECT audience, EXISTS (SELECT 1 FROM user_role_binding r WHERE r.audience = a.audience AND r.role = 'admin')
FROM (SELECT DISTINCT audience FROM user_audience) a;

INSERT INTO permission_definition (code, service, resource, action, status, source) VALUES
('system:account:list', 'system', 'account', 'list', 1, 'code'),
('system:account:query', 'system', 'account', 'query', 1, 'code'),
('system:account:assign', 'system', 'account', 'assign', 1, 'code');

-- +goose Down
-- 回滚前必须停止使用账号角色管理；删除审计表会丢失新增审计，先导出保存。
DELETE FROM permission_definition WHERE code IN ('system:account:list', 'system:account:query', 'system:account:assign');
DROP TABLE account_role_audit;
DROP TABLE user_audience;
DROP TABLE account_role_state;
