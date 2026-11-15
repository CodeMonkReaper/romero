-- name: InsertAuditEvent :exec
INSERT INTO audit_events (
    id,
    tenant_id,
    principal_id,
    agent_id,
    operation,
    resource_type,
    resource_id,
    scope_used,
    correlation_id,
    outcome,
    details,
    created_at
) VALUES (
    @id,
    @tenant_id,
    @principal_id,
    @agent_id,
    @operation,
    @resource_type,
    @resource_id,
    NULLIF(@scope_used::text, ''),
    NULLIF(@correlation_id::text, ''),
    @outcome,
    @details,
    @created_at
);
