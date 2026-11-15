-- name: GetMemoryByID :one
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
WHERE id = @id
  AND tenant_id = @tenant_id;

-- name: GetMemoryByIdempotencyKey :one
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
  AND idempotency_key = @idempotency_key;

-- name: LockActiveTopicMemory :one
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
  AND scope_type = @scope_type::scope_type_enum
  AND status = 'active'
  AND topic_key = @topic_key
  AND (
      (@scope_type::scope_type_enum IN ('org_shared', 'global_public'))
      OR (COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid) = COALESCE(sqlc.narg(project_id)::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
  )
  AND (
      (@scope_type::scope_type_enum <> 'agent_private')
      OR (owner_agent_id = sqlc.narg(owner_agent_id)::uuid)
  )
  AND (
      (@scope_type::scope_type_enum <> 'task_shared')
      OR (task_id = @task_id::text)
  )
LIMIT 1
FOR UPDATE;

-- name: InsertMemory :exec
INSERT INTO memories (
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
) VALUES (
    @id,
    @tenant_id,
    @project_id,
    @owner_agent_id,
    NULLIF(@task_id::text, ''),
    @memory_type,
    @scope_type,
    @status,
    NULLIF(@topic_key::text, ''),
    @summary,
    @content,
    @content_sha256,
    @token_count,
    @tags,
    @entities,
    @epistemic_status,
    @alpha_score,
    @beta_score,
    @importance,
    @utility_score,
    @access_count,
    @version,
    @nli_checked,
    @primary_source_id,
    NULLIF(@idempotency_key::text, ''),
    @observed_at,
    @valid_from,
    @valid_until,
    @created_at,
    @updated_at,
    @last_accessed_at,
    @metadata
);

-- name: UpdateMemoryState :execrows
UPDATE memories
SET
    status = @status,
    summary = @summary,
    content = @content,
    content_sha256 = @content_sha256,
    token_count = @token_count,
    tags = @tags,
    entities = @entities,
    epistemic_status = @epistemic_status,
    alpha_score = @alpha_score,
    beta_score = @beta_score,
    importance = @importance,
    utility_score = @utility_score,
    access_count = @access_count,
    version = @version,
    nli_checked = @nli_checked,
    valid_from = @valid_from,
    valid_until = @valid_until,
    updated_at = @updated_at,
    last_accessed_at = @last_accessed_at,
    metadata = @metadata
WHERE id = @id
  AND tenant_id = @tenant_id;

-- name: ListVersionsByMemoryID :many
SELECT
    id,
    memory_id,
    version,
    summary,
    content,
    tags,
    entities,
    metadata,
    epistemic_status,
    alpha_score,
    beta_score,
    importance,
    utility_score,
    status,
    valid_from,
    valid_until,
    changed_by_source,
    change_reason,
    created_at
FROM memory_versions
WHERE memory_id = @memory_id
ORDER BY version ASC;

-- name: InsertMemoryVersion :exec
INSERT INTO memory_versions (
    id,
    memory_id,
    version,
    summary,
    content,
    tags,
    entities,
    metadata,
    epistemic_status,
    alpha_score,
    beta_score,
    importance,
    utility_score,
    status,
    valid_from,
    valid_until,
    changed_by_source,
    change_reason,
    created_at
) VALUES (
    @id,
    @memory_id,
    @version,
    @summary,
    @content,
    @tags,
    @entities,
    @metadata,
    @epistemic_status,
    @alpha_score,
    @beta_score,
    @importance,
    @utility_score,
    @status,
    @valid_from,
    @valid_until,
    @changed_by_source,
    NULLIF(@change_reason::text, ''),
    @created_at
);

-- name: CloseMemoryVersionValidity :execrows
UPDATE memory_versions
SET valid_until = @valid_until::timestamptz
WHERE memory_id = @memory_id
  AND version = @version;

-- name: GetMemoryVersionPointInTime :one
SELECT
    id,
    memory_id,
    version,
    summary,
    content,
    tags,
    entities,
    metadata,
    epistemic_status,
    alpha_score,
    beta_score,
    importance,
    utility_score,
    status,
    valid_from,
    valid_until,
    changed_by_source,
    change_reason,
    created_at
FROM memory_versions
WHERE memory_id = @memory_id
  AND created_at <= @known_at::timestamptz
  AND valid_from <= @valid_at::timestamptz
  AND (valid_until IS NULL OR valid_until > @valid_at::timestamptz)
ORDER BY created_at DESC, version DESC
LIMIT 1;

-- name: ListActiveRelationsByMemoryID :many
SELECT
    id,
    tenant_id,
    project_id,
    from_memory_id,
    to_memory_id,
    relation_type,
    weight,
    nli_entailment,
    nli_contradiction,
    source_id,
    valid_from,
    valid_until,
    is_active,
    notes,
    created_at
FROM memory_relations
WHERE tenant_id = @tenant_id
  AND is_active = TRUE
  AND (from_memory_id = @memory_id OR to_memory_id = @memory_id)
ORDER BY created_at ASC, id ASC;

-- name: InsertMemoryRelation :exec
INSERT INTO memory_relations (
    id,
    tenant_id,
    project_id,
    from_memory_id,
    to_memory_id,
    relation_type,
    weight,
    nli_entailment,
    nli_contradiction,
    source_id,
    valid_from,
    valid_until,
    is_active,
    notes,
    created_at
) VALUES (
    @id,
    @tenant_id,
    @project_id,
    @from_memory_id,
    @to_memory_id,
    @relation_type,
    @weight,
    @nli_entailment,
    @nli_contradiction,
    @source_id,
    @valid_from,
    @valid_until,
    @is_active,
    NULLIF(@notes::text, ''),
    @created_at
);
