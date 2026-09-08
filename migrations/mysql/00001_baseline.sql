-- +goose NO TRANSACTION
-- +goose Up

CREATE TABLE permission_definition (
    id         bigint       NOT NULL AUTO_INCREMENT,
    code       varchar(128) NOT NULL,
    service    varchar(64)  NOT NULL,
    resource   varchar(64)  NOT NULL,
    action     varchar(64)  NOT NULL,
    status     int          NOT NULL DEFAULT 1,
    source     varchar(32)  NOT NULL DEFAULT 'manual',
    created_at datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_permission_definition_code (code),
    CONSTRAINT ck_permission_definition_status CHECK (status IN (0, 1))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_permission_definition_parts ON permission_definition (service, resource, action);

CREATE TABLE navigation_node (
    id              bigint       NOT NULL AUTO_INCREMENT,
    parent_id       bigint,
    name            varchar(64)  NOT NULL,
    permission_code varchar(128),
    type            int          NOT NULL,
    path            varchar(255) NOT NULL DEFAULT '',
    component       varchar(255) NOT NULL DEFAULT '',
    icon            varchar(64)  NOT NULL DEFAULT '',
    sort            int          NOT NULL DEFAULT 0,
    visible         boolean      NOT NULL DEFAULT true,
    status          int          NOT NULL DEFAULT 1,
    created_at      datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_navigation_node_permission_code (permission_code),
    CONSTRAINT fk_navigation_node_permission FOREIGN KEY (permission_code) REFERENCES permission_definition(code) ON DELETE SET NULL,
    CONSTRAINT fk_navigation_node_parent FOREIGN KEY (parent_id) REFERENCES navigation_node(id) ON DELETE RESTRICT
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_navigation_node_parent ON navigation_node (parent_id);

-- MySQL does not allow CHECK constraints to reference AUTO_INCREMENT columns.
-- Triggers preserve the same database-level self-parent invariant as PostgreSQL.
-- +goose StatementBegin
CREATE TRIGGER ck_navigation_node_not_self_parent_insert
BEFORE INSERT ON navigation_node
FOR EACH ROW
BEGIN
    IF NEW.parent_id IS NOT NULL AND NEW.parent_id = NEW.id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'navigation node cannot be its own parent';
    END IF;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ck_navigation_node_not_self_parent_update
BEFORE UPDATE ON navigation_node
FOR EACH ROW
BEGIN
    IF NEW.parent_id IS NOT NULL AND NEW.parent_id = NEW.id THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'navigation node cannot be its own parent';
    END IF;
END;
-- +goose StatementEnd

CREATE TABLE permission_tree_state (
    id         bigint      NOT NULL,
    revision   bigint      NOT NULL DEFAULT 1,
    updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT ck_permission_tree_state_id CHECK (id = 1),
    CONSTRAINT ck_permission_tree_state_revision CHECK (revision > 0)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
INSERT INTO permission_tree_state (id, revision) VALUES (1, 1);

CREATE TABLE casbin_rule (
    id    bigint       NOT NULL AUTO_INCREMENT,
    ptype varchar(8)   NOT NULL,
    v0    varchar(128) NOT NULL DEFAULT '',
    v1    varchar(128) NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    UNIQUE KEY uk_casbin_rule (ptype, v0, v1)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_casbin_rule_ptype_v0 ON casbin_rule (ptype, v0);

CREATE TABLE authz_policy_state (
    id         bigint      NOT NULL,
    version    bigint      NOT NULL DEFAULT 0,
    updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT ck_authz_policy_state_id CHECK (id = 1),
    CONSTRAINT ck_authz_policy_state_version CHECK (version >= 0)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
INSERT INTO authz_policy_state (id, version) VALUES (1, 1);

CREATE TABLE authz_policy_audit (
    id              bigint       NOT NULL AUTO_INCREMENT,
    policy_version  bigint       NOT NULL,
    action          varchar(64)  NOT NULL,
    target          varchar(256) NOT NULL,
    actor_subject   varchar(128) NOT NULL DEFAULT '',
    request_id      varchar(128) NOT NULL DEFAULT '',
    trace_id        varchar(64)  NOT NULL DEFAULT '',
    `before`        json         NOT NULL,
    `after`         json         NOT NULL,
    created_at      datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT ck_authz_policy_audit_version CHECK (policy_version >= 0)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_authz_policy_audit_version ON authz_policy_audit (policy_version);
CREATE INDEX idx_authz_policy_audit_target_created ON authz_policy_audit (target, created_at);
CREATE INDEX idx_authz_policy_audit_actor_created ON authz_policy_audit (actor_subject, created_at);

CREATE TABLE sys_dict_type (
    id         bigint      NOT NULL AUTO_INCREMENT,
    name       varchar(64) NOT NULL,
    type       varchar(64) NOT NULL,
    status     int         NOT NULL DEFAULT 1,
    remark     text        NOT NULL DEFAULT (''),
    created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_sys_dict_type_type (type)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;

CREATE TABLE sys_dict_data (
    id         bigint       NOT NULL AUTO_INCREMENT,
    dict_type  varchar(64)  NOT NULL,
    label      varchar(128) NOT NULL,
    value      varchar(128) NOT NULL,
    sort       int          NOT NULL DEFAULT 0,
    css_class  varchar(64)  NOT NULL DEFAULT '',
    is_default boolean      NOT NULL DEFAULT false,
    status     int          NOT NULL DEFAULT 1,
    remark     text         NOT NULL DEFAULT (''),
    created_at datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at datetime(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_sys_dict_data_type_value (dict_type, value),
    CONSTRAINT fk_sys_dict_data_type FOREIGN KEY (dict_type) REFERENCES sys_dict_type(type) ON UPDATE CASCADE ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_sys_dict_data_type_sort ON sys_dict_data (dict_type, sort);

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

CREATE TABLE auth_session (
    id                 varchar(32) NOT NULL,
    identity_id        bigint      NOT NULL,
    refresh_token_hash varchar(64) NOT NULL,
    expires_at         datetime(6) NOT NULL,
    revoked_at         datetime(6),
    created_at         datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at         datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_auth_session_refresh_token_hash (refresh_token_hash),
    CONSTRAINT fk_auth_session_identity FOREIGN KEY (identity_id) REFERENCES social_identity(id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin;
CREATE INDEX idx_auth_session_identity ON auth_session (identity_id);
CREATE INDEX idx_auth_session_expires ON auth_session (expires_at);

INSERT INTO permission_definition (code, service, resource, action, status, source) VALUES
    ('system:permission:add',    'system', 'permission', 'add',    1, 'baseline'),
    ('system:permission:query',  'system', 'permission', 'query',  1, 'baseline'),
    ('system:permission:list',   'system', 'permission', 'list',   1, 'baseline'),
    ('system:permission:edit',   'system', 'permission', 'edit',   1, 'baseline'),
    ('system:permission:remove', 'system', 'permission', 'remove', 1, 'baseline'),
    ('system:role:list',         'system', 'role',       'list',   1, 'baseline'),
    ('system:role:query',        'system', 'role',       'query',  1, 'baseline'),
    ('system:role:assign',       'system', 'role',       'assign', 1, 'baseline'),
    ('system:dict:add',          'system', 'dict',       'add',    1, 'baseline'),
    ('system:dict:list',         'system', 'dict',       'list',   1, 'baseline'),
    ('system:dict:edit',         'system', 'dict',       'edit',   1, 'baseline'),
    ('system:dict:remove',       'system', 'dict',       'remove', 1, 'baseline');

INSERT INTO navigation_node (id, parent_id, name, permission_code, type, path, component, icon, sort) VALUES
    (1,   NULL, '系统管理', NULL,                       1, '/system',    '',                  'settings', 1),
    (110, 1,    '角色权限', 'system:role:list',         2, 'role',       'system/role/index', 'peoples',  1),
    (111, 110,  '角色查询', 'system:role:query',        3, '',           '',                  '',         1),
    (112, 110,  '权限分配', 'system:role:assign',       3, '',           '',                  '',         2),
    (120, 1,    '权限管理', 'system:permission:list',   2, 'permission', 'system/perm/index', 'tree',     2),
    (121, 120,  '权限查询', 'system:permission:query',  3, '',           '',                  '',         1),
    (122, 120,  '权限新增', 'system:permission:add',    3, '',           '',                  '',         2),
    (123, 120,  '权限修改', 'system:permission:edit',   3, '',           '',                  '',         3),
    (124, 120,  '权限删除', 'system:permission:remove', 3, '',           '',                  '',         4),
    (130, 1,    '字典管理', 'system:dict:list',         2, 'dict',       'system/dict/index', 'dict',     3),
    (132, 130,  '字典新增', 'system:dict:add',          3, '',           '',                  '',         2),
    (133, 130,  '字典修改', 'system:dict:edit',         3, '',           '',                  '',         3),
    (134, 130,  '字典删除', 'system:dict:remove',       3, '',           '',                  '',         4);

INSERT INTO casbin_rule (ptype, v0, v1) VALUES
    ('p', 'admin', 'system:*'),
    ('p', 'user',  'system:role:list'),
    ('p', 'user',  'system:role:query'),
    ('p', 'user',  'system:permission:list'),
    ('p', 'user',  'system:permission:query'),
    ('p', 'user',  'system:dict:list'),
    ('g', 'admin', 'user');

INSERT INTO sys_dict_type (name, type, remark) VALUES
    ('通用状态', 'sys_common_status',   '启用/禁用'),
    ('权限类型', 'sys_permission_type', '目录/菜单/按钮'),
    ('是否',     'sys_yes_no',          '通用是否标识');

INSERT INTO sys_dict_data (dict_type, label, value, sort, css_class, is_default) VALUES
    ('sys_common_status',   '正常', '1', 1, 'success', true),
    ('sys_common_status',   '禁用', '0', 2, 'danger',  false),
    ('sys_permission_type', '目录', '1', 1, '',        true),
    ('sys_permission_type', '菜单', '2', 2, '',        false),
    ('sys_permission_type', '按钮', '3', 3, '',        false),
    ('sys_yes_no',          '是',   'Y', 1, '',        false),
    ('sys_yes_no',          '否',   'N', 2, '',        true);

-- +goose Down
DROP TABLE IF EXISTS auth_session;
DROP TABLE IF EXISTS social_identity;
DROP TABLE IF EXISTS sys_dict_data;
DROP TABLE IF EXISTS sys_dict_type;
DROP TABLE IF EXISTS authz_policy_audit;
DROP TABLE IF EXISTS authz_policy_state;
DROP TABLE IF EXISTS casbin_rule;
DROP TABLE IF EXISTS permission_tree_state;
DROP TABLE IF EXISTS navigation_node;
DROP TABLE IF EXISTS permission_definition;
