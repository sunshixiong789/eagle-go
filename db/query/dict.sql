-- ── 字典类型 ───────────────────────────────────────────

-- name: CreateDictType :one
INSERT INTO sys_dict_type (name, type, status, remark)
VALUES (@name, @type, @status, @remark)
RETURNING *;

-- name: GetDictTypeByID :one
SELECT * FROM sys_dict_type WHERE id = @id;

-- name: GetDictTypeByType :one
SELECT * FROM sys_dict_type WHERE type = @type;

-- name: ListDictTypes :many
SELECT * FROM sys_dict_type
WHERE (sqlc.narg('keyword')::text IS NULL
       OR name ILIKE '%' || sqlc.narg('keyword')::text || '%'
       OR type ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint)
ORDER BY id
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('page_offset');

-- name: CountDictTypes :one
SELECT count(*) FROM sys_dict_type
WHERE (sqlc.narg('keyword')::text IS NULL
       OR name ILIKE '%' || sqlc.narg('keyword')::text || '%'
       OR type ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint);

-- name: UpdateDictType :one
UPDATE sys_dict_type SET
    name       = @name,
    status     = @status,
    remark     = @remark,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteDictType :execrows
DELETE FROM sys_dict_type WHERE id = @id;

-- ── 字典数据 ───────────────────────────────────────────

-- name: CreateDictData :one
INSERT INTO sys_dict_data (dict_type, label, value, sort, css_class, is_default, status, remark)
VALUES (@dict_type, @label, @value, @sort, @css_class, @is_default, @status, @remark)
RETURNING *;

-- name: GetDictDataByID :one
SELECT * FROM sys_dict_data WHERE id = @id;

-- name: ListDictDataByType :many
-- 前端下拉框的主要来源，走 Redis 缓存，命中率高
SELECT * FROM sys_dict_data
WHERE dict_type = @dict_type AND status = 1
ORDER BY sort, id;

-- name: ListDictData :many
SELECT * FROM sys_dict_data
WHERE (sqlc.narg('dict_type')::text IS NULL OR dict_type = sqlc.narg('dict_type')::text)
  AND (sqlc.narg('keyword')::text IS NULL OR label ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint)
ORDER BY dict_type, sort, id
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('page_offset');

-- name: CountDictData :one
SELECT count(*) FROM sys_dict_data
WHERE (sqlc.narg('dict_type')::text IS NULL OR dict_type = sqlc.narg('dict_type')::text)
  AND (sqlc.narg('keyword')::text IS NULL OR label ILIKE '%' || sqlc.narg('keyword')::text || '%')
  AND (sqlc.narg('status')::smallint IS NULL OR status = sqlc.narg('status')::smallint);

-- name: UpdateDictData :one
UPDATE sys_dict_data SET
    label      = @label,
    value      = @value,
    sort       = @sort,
    css_class  = @css_class,
    is_default = @is_default,
    status     = @status,
    remark     = @remark,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteDictData :execrows
DELETE FROM sys_dict_data WHERE id = @id;
