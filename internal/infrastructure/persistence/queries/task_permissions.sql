-- name: EvaluateTaskAccessSnapshot :one
SELECT
    (
        pr.id = @project_id::uuid
        AND p.allowed_projects IS NOT NULL
        AND @project_id::uuid = ANY(p.allowed_projects)
        AND 'task_shared' = ANY(p.allowed_scopes)
        AND COALESCE(tp.can_read, FALSE)
    )::boolean AS can_read,
    (
        pr.id = @project_id::uuid
        AND p.allowed_projects IS NOT NULL
        AND @project_id::uuid = ANY(p.allowed_projects)
        AND 'task_shared' = ANY(p.allowed_scopes)
        AND COALESCE(tp.can_write, FALSE)
    )::boolean AS can_write
FROM principals p
JOIN tenants t
  ON t.id = p.tenant_id
 AND t.is_active = TRUE
JOIN projects pr
  ON pr.tenant_id = p.tenant_id
 AND pr.id = @project_id::uuid
 AND pr.is_active = TRUE
LEFT JOIN task_permissions tp
  ON tp.tenant_id = p.tenant_id
 AND tp.project_id = pr.id
 AND tp.principal_id = p.id
 AND tp.task_id = @task_id::text
WHERE p.id = @principal_id::uuid
  AND p.tenant_id = @tenant_id::uuid
  AND p.is_active = TRUE
  AND (p.expires_at IS NULL OR p.expires_at > NOW());

-- name: UpsertTaskPermission :one
INSERT INTO task_permissions (
    tenant_id,
    project_id,
    task_id,
    principal_id,
    can_read,
    can_write
) VALUES (
    @tenant_id,
    @project_id,
    @task_id,
    @principal_id,
    @can_read,
    @can_write
)
ON CONFLICT (tenant_id, project_id, task_id, principal_id)
DO UPDATE SET
    can_read = EXCLUDED.can_read,
    can_write = EXCLUDED.can_write,
    updated_at = NOW()
RETURNING id, tenant_id, project_id, task_id, principal_id, can_read, can_write, created_at, updated_at;
