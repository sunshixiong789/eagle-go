-- name: CreatePermission :one
INSERT INTO sys_permission (parent_id, name, code, type, path, component, icon, sort, visible, status)
VALUES (@parent_id, @name, @code, @type, @path, @component, @icon, @sort, @visible, @status)
RETURNING *;

-- name: GetPermissionByID :one
SELECT * FROM sys_permission WHERE id = @id;

-- name: ListPermissions :many
-- 权限总量有限（百级），一次全量取出在应用层拼树，比递归 CTE 更简单也更快
SELECT * FROM sys_permission
WHERE (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint)
  AND (sqlc.narg('type')::smallint IS NULL OR type = sqlc.narg('type')::smallint)
ORDER BY parent_id, sort, id;

-- name: UpdatePermission :one
UPDATE sys_permission SET
    parent_id  = @parent_id,
    name       = @name,
    code       = @code,
    type       = @type,
    path       = @path,
    component  = @component,
    icon       = @icon,
    sort       = @sort,
    visible    = @visible,
    status     = @status,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeletePermission :execrows
DELETE FROM sys_permission WHERE id = @id;

-- name: CountChildPermissions :one
SELECT count(*) FROM sys_permission WHERE parent_id = @parent_id;

-- ── 鉴权热路径 ─────────────────────────────────────────

-- name: ListPermissionCodesByUserID :many
-- authz 中间件的数据来源。结果会进 Redis 缓存，这里只在缓存未命中时走。
SELECT DISTINCT p.code
FROM sys_permission p
JOIN sys_role_permission rp ON rp.permission_id = p.id
JOIN sys_user_role ur       ON ur.role_id = rp.role_id
JOIN sys_role r             ON r.id = ur.role_id
WHERE ur.user_id = @user_id
  AND p.code <> ''
  AND p.status = 1
  AND r.status = 1
  AND r.deleted_at IS NULL;

-- name: ListPermissionsByUserID :many
-- 构建前端菜单树用（含目录/菜单，不只是权限码）
SELECT DISTINCT p.*
FROM sys_permission p
JOIN sys_role_permission rp ON rp.permission_id = p.id
JOIN sys_user_role ur       ON ur.role_id = rp.role_id
JOIN sys_role r             ON r.id = ur.role_id
WHERE ur.user_id = @user_id
  AND p.status = 1
  AND r.status = 1
  AND r.deleted_at IS NULL
ORDER BY p.parent_id, p.sort, p.id;

-- ── 角色 ↔ 权限 ────────────────────────────────────────

-- name: ListPermissionIDsByRoleID :many
SELECT permission_id FROM sys_role_permission WHERE role_id = @role_id;

-- name: DeleteRolePermissions :exec
DELETE FROM sys_role_permission WHERE role_id = @role_id;

-- name: AddRolePermission :exec
INSERT INTO sys_role_permission (role_id, permission_id) VALUES (@role_id, @permission_id)
ON CONFLICT DO NOTHING;

-- name: ListUserIDsByRoleID :many
-- 角色权限变更后，用来精准失效这批用户的权限缓存
SELECT user_id FROM sys_user_role WHERE role_id = @role_id;
