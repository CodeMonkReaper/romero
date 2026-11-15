-- +goose Up
-- +goose StatementBegin

ALTER TABLE projects
    ADD CONSTRAINT uq_projects_tenant_id UNIQUE (tenant_id, id);

ALTER TABLE principals
    ADD CONSTRAINT uq_principals_tenant_id UNIQUE (tenant_id, id);

CREATE TABLE task_permissions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id   UUID NOT NULL,
    task_id      VARCHAR(128) NOT NULL CHECK (btrim(task_id) <> ''),
    principal_id UUID NOT NULL,
    can_read     BOOLEAN NOT NULL DEFAULT FALSE,
    can_write    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_task_permissions_project_tenant
        FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_task_permissions_principal_tenant
        FOREIGN KEY (tenant_id, principal_id) REFERENCES principals(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT uq_task_permissions_tuple UNIQUE (tenant_id, project_id, task_id, principal_id)
);

CREATE TABLE opaque_memory_requests (
    request_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id         UUID NOT NULL,
    task_id            VARCHAR(128) NOT NULL CHECK (btrim(task_id) <> ''),
    principal_id       UUID NOT NULL,
    operation          VARCHAR(32) NOT NULL CHECK (operation IN ('capture', 'update', 'supersede', 'archive', 'feedback')),
    target_memory_id   UUID,
    payload_version    SMALLINT NOT NULL DEFAULT 1 CHECK (payload_version >= 1),
    payload            JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_sha256     CHAR(64) NOT NULL,
    idempotency_key    VARCHAR(128),
    status             VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    attempt_count      INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_retries        INTEGER NOT NULL DEFAULT 3 CHECK (max_retries >= 0),
    next_attempt_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_token        UUID,
    lease_expires_at   TIMESTAMPTZ,
    last_error_code    VARCHAR(64),
    last_error_message TEXT,
    result_memory_id   UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at       TIMESTAMPTZ,
    CONSTRAINT fk_opaque_requests_project_tenant
        FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_opaque_requests_principal_tenant
        FOREIGN KEY (tenant_id, principal_id) REFERENCES principals(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_opaque_terminal_completed_at CHECK (
        (status IN ('completed', 'failed') AND completed_at IS NOT NULL) OR
        (status IN ('pending', 'running') AND completed_at IS NULL)
    ),
    CONSTRAINT chk_opaque_running_lease CHECK (
        (status = 'running' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL) OR
        (status <> 'running' AND lease_token IS NULL AND lease_expires_at IS NULL)
    )
);

CREATE UNIQUE INDEX idx_opaque_requests_idempotency
    ON opaque_memory_requests(tenant_id, principal_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX idx_opaque_requests_claim
    ON opaque_memory_requests(status, next_attempt_at ASC, lease_expires_at ASC)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_opaque_requests_purge
    ON opaque_memory_requests(completed_at)
    WHERE status IN ('completed', 'failed');

LOCK TABLE memories IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    dup_count INTEGER;
BEGIN
    SELECT COUNT(*)
    INTO dup_count
    FROM (
        SELECT tenant_id, scope_type, topic_key
        FROM memories
        WHERE status = 'active'
          AND topic_key IS NOT NULL
          AND scope_type IN ('org_shared', 'global_public')
        GROUP BY tenant_id, scope_type, topic_key
        HAVING COUNT(*) > 1
    ) dups;

    IF dup_count > 0 THEN
        RAISE EXCEPTION 'cannot normalize idx_memories_active_topic_key: % active duplicate (tenant_id, scope_type, topic_key) groups exist in org_shared/global_public', dup_count;
    END IF;
END $$;

DROP INDEX idx_memories_active_topic_key;

CREATE UNIQUE INDEX idx_memories_active_topic_key
    ON memories(
        tenant_id,
        (CASE WHEN scope_type IN ('org_shared', 'global_public') THEN '00000000-0000-0000-0000-000000000000'::uuid ELSE COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid) END),
        scope_type,
        (CASE WHEN scope_type = 'agent_private' THEN COALESCE(owner_agent_id, '00000000-0000-0000-0000-000000000000'::uuid) ELSE '00000000-0000-0000-0000-000000000000'::uuid END),
        (CASE WHEN scope_type = 'task_shared' THEN COALESCE(task_id, '') ELSE '' END),
        topic_key
    )
    WHERE status = 'active' AND topic_key IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

LOCK TABLE memories IN SHARE ROW EXCLUSIVE MODE;

DROP INDEX IF EXISTS idx_memories_active_topic_key;

CREATE UNIQUE INDEX idx_memories_active_topic_key
    ON memories(
        tenant_id,
        COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid),
        scope_type,
        (CASE WHEN scope_type = 'agent_private' THEN COALESCE(owner_agent_id, '00000000-0000-0000-0000-000000000000'::uuid) ELSE '00000000-0000-0000-0000-000000000000'::uuid END),
        (CASE WHEN scope_type = 'task_shared' THEN COALESCE(task_id, '') ELSE '' END),
        topic_key
    )
    WHERE status = 'active' AND topic_key IS NOT NULL;

DROP TABLE IF EXISTS opaque_memory_requests;
DROP TABLE IF EXISTS task_permissions;

ALTER TABLE principals DROP CONSTRAINT IF EXISTS uq_principals_tenant_id;
ALTER TABLE projects DROP CONSTRAINT IF EXISTS uq_projects_tenant_id;

-- +goose StatementEnd
