-- +goose Up
-- +goose StatementBegin

-- 后端授权契约目录。Proto 上声明的权限码必须存在于本表。
CREATE TABLE permission_definition (
    id         bigserial    PRIMARY KEY,
    code       varchar(128) NOT NULL UNIQUE,
    service    varchar(64)  NOT NULL,
    resource   varchar(64)  NOT NULL,
    action     varchar(64)  NOT NULL,
    status     int          NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    source     varchar(32)  NOT NULL DEFAULT 'manual',
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_permission_definition_parts ON permission_definition (service, resource, action);

-- 前端导航树。permission_code 只能引用后端已定义的权限码。
CREATE TABLE navigation_node (
    id              bigserial    PRIMARY KEY,
    parent_id       bigint,
    name            varchar(64)  NOT NULL,
    permission_code varchar(128) UNIQUE REFERENCES permission_definition(code) ON DELETE SET NULL,
    type            int          NOT NULL,
    path            varchar(255) NOT NULL DEFAULT '',
    component       varchar(255) NOT NULL DEFAULT '',
    icon            varchar(64)  NOT NULL DEFAULT '',
    sort            int          NOT NULL DEFAULT 0,
    visible         boolean      NOT NULL DEFAULT true,
    status          int          NOT NULL DEFAULT 1,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT fk_navigation_node_parent FOREIGN KEY (parent_id) REFERENCES navigation_node(id) ON DELETE RESTRICT,
    CONSTRAINT ck_navigation_node_not_self_parent CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX idx_navigation_node_parent ON navigation_node (parent_id);

-- 权限树的全局乐观并发版本与事务串行化锁。
CREATE TABLE permission_tree_state (
    id         bigint      PRIMARY KEY CHECK (id = 1),
    revision   bigint      NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO permission_tree_state (id, revision) VALUES (1, 1);

-- Casbin 策略表：p 表示角色权限，g 表示角色继承。
CREATE TABLE casbin_rule (
    id    bigserial    PRIMARY KEY,
    ptype varchar(8)   NOT NULL,
    v0    varchar(128) NOT NULL DEFAULT '',
    v1    varchar(128) NOT NULL DEFAULT '',
    v2    varchar(128) NOT NULL DEFAULT '',
    v3    varchar(128) NOT NULL DEFAULT '',
    v4    varchar(128) NOT NULL DEFAULT '',
    v5    varchar(128) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX uk_casbin_rule ON casbin_rule (ptype, v0, v1, v2, v3, v4, v5);
CREATE INDEX idx_casbin_rule_ptype_v0 ON casbin_rule (ptype, v0);

-- 多副本授权策略对账版本。
CREATE TABLE authz_policy_state (
    id         bigint      PRIMARY KEY CHECK (id = 1),
    version    bigint      NOT NULL DEFAULT 0 CHECK (version >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO authz_policy_state (id, version) VALUES (1, 1);

-- 授权变更审计记录，只追加不更新。
CREATE TABLE authz_policy_audit (
    id              bigserial    PRIMARY KEY,
    policy_version  bigint       NOT NULL CHECK (policy_version >= 0),
    action          varchar(64)  NOT NULL,
    target          varchar(256) NOT NULL,
    actor_subject   varchar(128) NOT NULL DEFAULT '',
    actor_client_id varchar(128) NOT NULL DEFAULT '',
    request_id      varchar(128) NOT NULL DEFAULT '',
    trace_id        varchar(64)  NOT NULL DEFAULT '',
    before          jsonb        NOT NULL DEFAULT '[]'::jsonb,
    after           jsonb        NOT NULL DEFAULT '[]'::jsonb,
    created_at      timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_authz_policy_audit_version ON authz_policy_audit (policy_version);
CREATE INDEX idx_authz_policy_audit_target_created ON authz_policy_audit (target, created_at);
CREATE INDEX idx_authz_policy_audit_actor_created ON authz_policy_audit (actor_subject, created_at);

-- 最小 CRUD 示例：字典类型和字典项。
CREATE TABLE sys_dict_type (
    id         bigserial   PRIMARY KEY,
    name       varchar(64) NOT NULL,
    type       varchar(64) NOT NULL UNIQUE,
    status     int         NOT NULL DEFAULT 1,
    remark     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sys_dict_data (
    id         bigserial    PRIMARY KEY,
    dict_type  varchar(64)  NOT NULL REFERENCES sys_dict_type(type) ON UPDATE CASCADE ON DELETE CASCADE,
    label      varchar(128) NOT NULL,
    value      varchar(128) NOT NULL,
    sort       int          NOT NULL DEFAULT 0,
    css_class  varchar(64)  NOT NULL DEFAULT '',
    is_default boolean      NOT NULL DEFAULT false,
    status     int          NOT NULL DEFAULT 1,
    remark     text         NOT NULL DEFAULT '',
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now(),
    UNIQUE (dict_type, value)
);
CREATE INDEX idx_sys_dict_data_type_sort ON sys_dict_data (dict_type, sort);

-- 只种入当前 Proto 实际声明的权限码。
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

-- type: 1=目录、2=菜单、3=按钮。
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
SELECT setval(pg_get_serial_sequence('navigation_node', 'id'), (SELECT max(id) FROM navigation_node));

-- 角色由 Eagle 令牌提供；本库只保存角色与权限的关系。
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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sys_dict_data;
DROP TABLE IF EXISTS sys_dict_type;
DROP TABLE IF EXISTS authz_policy_audit;
DROP TABLE IF EXISTS authz_policy_state;
DROP TABLE IF EXISTS casbin_rule;
DROP TABLE IF EXISTS permission_tree_state;
DROP TABLE IF EXISTS navigation_node;
DROP TABLE IF EXISTS permission_definition;
-- +goose StatementEnd
