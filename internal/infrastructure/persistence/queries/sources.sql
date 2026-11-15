-- name: InsertSource :exec
INSERT INTO memory_sources (
    id,
    tenant_id,
    project_id,
    source_type,
    external_source_id,
    actor_principal_id,
    actor_agent_id,
    uri,
    raw_excerpt_hash,
    metadata,
    created_at
) VALUES (
    @id,
    @tenant_id,
    @project_id,
    @source_type,
    @external_source_id,
    @actor_principal_id,
    @actor_agent_id,
    NULLIF(@uri::text, ''),
    NULLIF(@raw_excerpt_hash::text, ''),
    @metadata,
    @created_at
);
