-- name: ListREMConsolidationCandidates :many
SELECT
    m.id,
    m.tenant_id,
    m.project_id,
    m.owner_agent_id,
    m.task_id,
    m.memory_type,
    m.scope_type,
    m.status,
    m.topic_key,
    m.summary,
    m.content,
    m.content_sha256,
    m.token_count,
    m.tags,
    m.entities,
    m.epistemic_status,
    m.alpha_score,
    m.beta_score,
    m.confidence,
    m.importance,
    m.utility_score,
    m.access_count,
    m.version,
    m.nli_checked,
    m.primary_source_id,
    m.idempotency_key,
    m.observed_at,
    m.valid_from,
    m.valid_until,
    m.created_at,
    m.updated_at,
    m.last_accessed_at,
    m.metadata,
    me.dense_vec::text AS dense_vec_text
FROM memories m
JOIN memory_embeddings me ON me.memory_id = m.id AND me.tenant_id = m.tenant_id
WHERE m.tenant_id = @tenant_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR m.project_id = sqlc.narg('project_id')::uuid)
  AND m.memory_type = 'episodic'
  AND m.status = 'active'
  AND m.confidence >= @min_confidence::float4
  AND m.valid_until IS NULL
ORDER BY m.observed_at ASC, m.id ASC;

-- name: ListActiveVerifiedByProject :many
SELECT
    id,
    tenant_id,
    project_id,
    owner_agent_id,
    task_id,
    memory_type,
    scope_type,
    status,
    topic_key,
    summary,
    content,
    content_sha256,
    token_count,
    tags,
    entities,
    epistemic_status,
    alpha_score,
    beta_score,
    confidence,
    importance,
    utility_score,
    access_count,
    version,
    nli_checked,
    primary_source_id,
    idempotency_key,
    observed_at,
    valid_from,
    valid_until,
    created_at,
    updated_at,
    last_accessed_at,
    metadata
FROM memories
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id')::uuid)
  AND status = 'active'
  AND epistemic_status = 'verified'
  AND valid_from <= @now
  AND (valid_until IS NULL OR valid_until > @now)
ORDER BY observed_at DESC, id ASC;

-- name: ListActiveREMTargets :many
SELECT DISTINCT ON (t.id)
    t.id AS tenant_id,
    p.id AS principal_id
FROM tenants t
JOIN principals p ON p.tenant_id = t.id
WHERE t.is_active = TRUE
  AND p.is_active = TRUE
  AND (p.expires_at IS NULL OR p.expires_at > NOW())
ORDER BY t.id,
         CASE WHEN p.principal_type IN ('user', 'service') THEN 0 ELSE 1 END,
         p.created_at ASC,
         p.id ASC;
