-- name: CreateRole :one
INSERT INTO sys_role (name, code, sort, data_scope, status, remark)
VALUES (@name, @code, @sort, @data_scope, @status, @remark)
RETURNING *;

-- name: GetRoleByID :one
SELECT * FROM sys_role WHERE id = @id AND deleted_at IS NULL;

-- name: GetRoleByCode :one
SELECT * FROM sys_role WHERE code = @code AND deleted_at IS NULL;

-- name: ListRoles :many
SELECT * FROM sys_role
WHERE deleted_at IS NULL
  AND (sqlc.narg('keyword')::text IS NULL
       OR name ILIKE '%' || sqlc.narg('keyword')::text || '%'
       OR code ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint)
ORDER BY sort, id
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('page_offset');

-- name: CountRoles :one
SELECT count(*) FROM sys_role
WHERE deleted_at IS NULL
  AND (sqlc.narg('keyword')::text IS NULL
       OR name ILIKE '%' || sqlc.narg('keyword')::text || '%'
       OR code ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint);

-- name: UpdateRole :one
UPDATE sys_role SET
    name       = @name,
    sort       = @sort,
    data_scope = @data_scope,
    status     = @status,
    remark     = @remark,
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteRole :execrows
UPDATE sys_role SET deleted_at = now(), updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- ── 用户 ↔ 角色 ────────────────────────────────────────

-- name: ListRolesByUserID :many
SELECT r.* FROM sys_role r
JOIN sys_user_role ur ON ur.role_id = r.id
WHERE ur.user_id = @user_id AND r.deleted_at IS NULL AND r.status = 1
ORDER BY r.sort, r.id;

-- name: ListRoleCodesByUserID :many
SELECT r.code FROM sys_role r
JOIN sys_user_role ur ON ur.role_id = r.id
WHERE ur.user_id = @user_id AND r.deleted_at IS NULL AND r.status = 1;

-- name: DeleteUserRoles :exec
DELETE FROM sys_user_role WHERE user_id = @user_id;

-- name: AddUserRole :exec
INSERT INTO sys_user_role (user_id, role_id) VALUES (@user_id, @role_id)
ON CONFLICT DO NOTHING;

-- name: CountUsersByRoleID :one
SELECT count(*) FROM sys_user_role ur
JOIN sys_user u ON u.id = ur.user_id
WHERE ur.role_id = @role_id AND u.deleted_at IS NULL;
