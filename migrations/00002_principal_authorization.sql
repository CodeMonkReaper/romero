-- +goose Up
-- Existing invalid scope values deliberately abort this migration. An operator
-- must review their intended grants; read/write cannot be mapped to scope names.
ALTER TABLE principals ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE principals ALTER COLUMN allowed_scopes SET DEFAULT '{}'::text[];
ALTER TABLE principals ADD CONSTRAINT chk_principals_allowed_scopes CHECK (
    allowed_scopes <@ ARRAY['agent_private', 'task_shared', 'project_shared', 'org_shared', 'global_public']::text[]
    AND array_position(allowed_scopes, NULL) IS NULL
);

-- +goose Down
ALTER TABLE principals DROP CONSTRAINT chk_principals_allowed_scopes;
ALTER TABLE principals ALTER COLUMN allowed_scopes SET DEFAULT ARRAY['read', 'write'];
ALTER TABLE principals DROP COLUMN is_admin;
