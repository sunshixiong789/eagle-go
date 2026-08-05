-- +goose Up
-- +goose StatementBegin

-- 用户
CREATE TABLE sys_user (
    id            bigserial    PRIMARY KEY,
    username      varchar(64)  NOT NULL,
    password_hash text         NOT NULL,
    nickname      varchar(64)  NOT NULL DEFAULT '',
    email         varchar(128) NOT NULL DEFAULT '',
    phone         varchar(32)  NOT NULL DEFAULT '',
    dept_id       bigint       NOT NULL DEFAULT 0,
    -- 0=禁用 1=正常
    status        smallint     NOT NULL DEFAULT 1,
    remark        text         NOT NULL DEFAULT '',
    last_login_at timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz  NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);
-- 软删除后允许同名重新注册，所以用条件唯一索引而不是唯一约束
CREATE UNIQUE INDEX uk_sys_user_username ON sys_user (username) WHERE deleted_at IS NULL;
CREATE INDEX idx_sys_user_dept ON sys_user (dept_id) WHERE deleted_at IS NULL;

-- 角色
CREATE TABLE sys_role (
    id         bigserial   PRIMARY KEY,
    name       varchar(64) NOT NULL,
    code       varchar(64) NOT NULL,
    sort       int         NOT NULL DEFAULT 0,
    -- 数据权限范围：1=全部 2=本部门及以下 3=本部门 4=仅本人
    -- 本期只落库不生效，等接入 Casbin 做数据权限时再消费
    data_scope smallint    NOT NULL DEFAULT 1,
    status     smallint    NOT NULL DEFAULT 1,
    remark     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX uk_sys_role_code ON sys_role (code) WHERE deleted_at IS NULL;

-- 权限（目录 / 菜单 / 按钮三合一的树）
CREATE TABLE sys_permission (
    id         bigserial    PRIMARY KEY,
    parent_id  bigint       NOT NULL DEFAULT 0,
    name       varchar(64)  NOT NULL,
    -- 权限码，如 system:user:add。目录/菜单可为空，按钮必填
    code       varchar(128) NOT NULL DEFAULT '',
    -- 1=目录 2=菜单 3=按钮
    type       smallint     NOT NULL,
    path       varchar(255) NOT NULL DEFAULT '',
    component  varchar(255) NOT NULL DEFAULT '',
    icon       varchar(64)  NOT NULL DEFAULT '',
    sort       int          NOT NULL DEFAULT 0,
    visible    boolean      NOT NULL DEFAULT true,
    status     smallint     NOT NULL DEFAULT 1,
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uk_sys_permission_code ON sys_permission (code) WHERE code <> '';
CREATE INDEX idx_sys_permission_parent ON sys_permission (parent_id);

-- 用户-角色
CREATE TABLE sys_user_role (
    user_id bigint NOT NULL REFERENCES sys_user (id) ON DELETE CASCADE,
    role_id bigint NOT NULL REFERENCES sys_role (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX idx_sys_user_role_role ON sys_user_role (role_id);

-- 角色-权限
CREATE TABLE sys_role_permission (
    role_id       bigint NOT NULL REFERENCES sys_role (id) ON DELETE CASCADE,
    permission_id bigint NOT NULL REFERENCES sys_permission (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);
CREATE INDEX idx_sys_role_permission_perm ON sys_role_permission (permission_id);

-- 字典类型
CREATE TABLE sys_dict_type (
    id         bigserial   PRIMARY KEY,
    name       varchar(64) NOT NULL,
    type       varchar(64) NOT NULL UNIQUE,
    status     smallint    NOT NULL DEFAULT 1,
    remark     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- 字典数据
CREATE TABLE sys_dict_data (
    id         bigserial    PRIMARY KEY,
    dict_type  varchar(64)  NOT NULL REFERENCES sys_dict_type (type) ON UPDATE CASCADE ON DELETE CASCADE,
    label      varchar(128) NOT NULL,
    value      varchar(128) NOT NULL,
    sort       int          NOT NULL DEFAULT 0,
    css_class  varchar(64)  NOT NULL DEFAULT '',
    is_default boolean      NOT NULL DEFAULT false,
    status     smallint     NOT NULL DEFAULT 1,
    remark     text         NOT NULL DEFAULT '',
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now(),
    UNIQUE (dict_type, value)
);
CREATE INDEX idx_sys_dict_data_type_sort ON sys_dict_data (dict_type, sort);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sys_dict_data;
DROP TABLE IF EXISTS sys_dict_type;
DROP TABLE IF EXISTS sys_role_permission;
DROP TABLE IF EXISTS sys_user_role;
DROP TABLE IF EXISTS sys_permission;
DROP TABLE IF EXISTS sys_role;
DROP TABLE IF EXISTS sys_user;
-- +goose StatementEnd
