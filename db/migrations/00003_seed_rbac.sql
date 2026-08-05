-- +goose Up
-- +goose StatementBegin

-- 只种入不含任何机密的基础数据。
-- 管理员账号刻意不在迁移里创建：迁移文件会进版本库，
-- 内置已知密码的账号会一路带到生产。改用 `go run ./cmd/eagle create-admin` 建号。

-- ── 角色 ──────────────────────────────────────────────
INSERT INTO sys_role (id, name, code, sort, data_scope, remark) VALUES
    (1, '超级管理员', 'admin',  1, 1, '拥有全部权限，鉴权中间件直接短路放行'),
    (2, '普通用户',   'common', 2, 4, '仅本人数据范围');

-- ── 权限树 ────────────────────────────────────────────
-- type: 1=目录 2=菜单 3=按钮
INSERT INTO sys_permission (id, parent_id, name, code, type, path, component, icon, sort) VALUES
    (1,   0,   '系统管理', '',                       1, '/system',    '',                    'settings', 1),

    (100, 1,   '用户管理', 'system:user:list',       2, 'user',       'system/user/index',   'user',     1),
    (101, 100, '用户查询', 'system:user:query',      3, '', '', '', 1),
    (102, 100, '用户新增', 'system:user:add',        3, '', '', '', 2),
    (103, 100, '用户修改', 'system:user:edit',       3, '', '', '', 3),
    (104, 100, '用户删除', 'system:user:remove',     3, '', '', '', 4),
    (105, 100, '重置密码', 'system:user:resetPwd',   3, '', '', '', 5),

    (110, 1,   '角色管理', 'system:role:list',       2, 'role',       'system/role/index',   'peoples',  2),
    (111, 110, '角色查询', 'system:role:query',      3, '', '', '', 1),
    (112, 110, '角色新增', 'system:role:add',        3, '', '', '', 2),
    (113, 110, '角色修改', 'system:role:edit',       3, '', '', '', 3),
    (114, 110, '角色删除', 'system:role:remove',     3, '', '', '', 4),

    (120, 1,   '权限管理', 'system:permission:list', 2, 'permission', 'system/perm/index',   'tree',     3),
    (121, 120, '权限查询', 'system:permission:query',3, '', '', '', 1),
    (122, 120, '权限新增', 'system:permission:add',  3, '', '', '', 2),
    (123, 120, '权限修改', 'system:permission:edit', 3, '', '', '', 3),
    (124, 120, '权限删除', 'system:permission:remove',3,'', '', '', 4),

    (130, 1,   '字典管理', 'system:dict:list',       2, 'dict',       'system/dict/index',   'dict',     4),
    (131, 130, '字典查询', 'system:dict:query',      3, '', '', '', 1),
    (132, 130, '字典新增', 'system:dict:add',        3, '', '', '', 2),
    (133, 130, '字典修改', 'system:dict:edit',       3, '', '', '', 3),
    (134, 130, '字典删除', 'system:dict:remove',     3, '', '', '', 4);

-- 普通用户默认只给查询类权限
INSERT INTO sys_role_permission (role_id, permission_id)
SELECT 2, id FROM sys_permission WHERE code LIKE '%:query' OR code LIKE '%:list';

-- 显式指定过 id，把序列推到最大值之后，否则后续 INSERT 会主键冲突
SELECT setval(pg_get_serial_sequence('sys_role', 'id'),       (SELECT max(id) FROM sys_role));
SELECT setval(pg_get_serial_sequence('sys_permission', 'id'), (SELECT max(id) FROM sys_permission));

-- ── 字典 ──────────────────────────────────────────────
INSERT INTO sys_dict_type (name, type, remark) VALUES
    ('通用状态',   'sys_common_status',   '启用/禁用'),
    ('权限类型',   'sys_permission_type', '目录/菜单/按钮'),
    ('数据权限',   'sys_data_scope',      '角色的数据可见范围'),
    ('是否',       'sys_yes_no',          '通用是否标识');

INSERT INTO sys_dict_data (dict_type, label, value, sort, css_class, is_default) VALUES
    ('sys_common_status',   '正常',         '1', 1, 'success', true),
    ('sys_common_status',   '禁用',         '0', 2, 'danger',  false),

    ('sys_permission_type', '目录',         '1', 1, '', true),
    ('sys_permission_type', '菜单',         '2', 2, '', false),
    ('sys_permission_type', '按钮',         '3', 3, '', false),

    ('sys_data_scope',      '全部数据',     '1', 1, '', true),
    ('sys_data_scope',      '本部门及以下', '2', 2, '', false),
    ('sys_data_scope',      '本部门',       '3', 3, '', false),
    ('sys_data_scope',      '仅本人',       '4', 4, '', false),

    ('sys_yes_no',          '是',           'Y', 1, '', false),
    ('sys_yes_no',          '否',           'N', 2, '', true);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM sys_dict_data  WHERE dict_type IN ('sys_common_status','sys_permission_type','sys_data_scope','sys_yes_no');
DELETE FROM sys_dict_type  WHERE type      IN ('sys_common_status','sys_permission_type','sys_data_scope','sys_yes_no');
DELETE FROM sys_role_permission WHERE role_id IN (1, 2);
DELETE FROM sys_permission WHERE id <= 200;
DELETE FROM sys_role       WHERE id IN (1, 2);
-- +goose StatementEnd
