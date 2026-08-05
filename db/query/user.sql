-- name: CreateUser :one
INSERT INTO sys_user (username, password_hash, nickname, email, phone, dept_id, status, remark)
VALUES (@username, @password_hash, @nickname, @email, @phone, @dept_id, @status, @remark)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM sys_user
WHERE id = @id AND deleted_at IS NULL;

-- name: GetUserByUsername :one
SELECT * FROM sys_user
WHERE username = @username AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM sys_user
WHERE deleted_at IS NULL
  AND (sqlc.narg('keyword')::text IS NULL
       OR username ILIKE '%' || sqlc.narg('keyword')::text || '%'
       OR nickname ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint)
  AND (sqlc.narg('dept_id')::bigint IS NULL OR dept_id = sqlc.narg('dept_id')::bigint)
ORDER BY id
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('page_offset');

-- name: CountUsers :one
SELECT count(*) FROM sys_user
WHERE deleted_at IS NULL
  AND (sqlc.narg('keyword')::text IS NULL
       OR username ILIKE '%' || sqlc.narg('keyword')::text || '%'
       OR nickname ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint)
  AND (sqlc.narg('dept_id')::bigint IS NULL OR dept_id = sqlc.narg('dept_id')::bigint);

-- name: UpdateUser :one
UPDATE sys_user SET
    nickname   = @nickname,
    email      = @email,
    phone      = @phone,
    dept_id    = @dept_id,
    status     = @status,
    remark     = @remark,
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: UpdateUserPassword :execrows
UPDATE sys_user SET password_hash = @password_hash, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: TouchUserLastLogin :exec
UPDATE sys_user SET last_login_at = now() WHERE id = @id;

-- name: SoftDeleteUser :execrows
UPDATE sys_user SET deleted_at = now(), updated_at = now()
WHERE id = @id AND deleted_at IS NULL;
