-- +goose Up
-- +goose StatementBegin
INSERT INTO permission_definition (code, service, resource, action, status, source) VALUES
    ('system:file:add', 'system', 'file', 'add', 1, 'migration'),
    ('system:file:remove', 'system', 'file', 'remove', 1, 'migration')
ON CONFLICT (code) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM permission_definition WHERE code IN (
    'system:file:add', 'system:file:remove'
);
-- +goose StatementEnd
