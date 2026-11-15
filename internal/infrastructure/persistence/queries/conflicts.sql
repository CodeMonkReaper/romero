-- name: FindSameScopeSemanticNeighbors :many
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
    m.metadata
FROM memories m
JOIN memory_embeddings me ON me.memory_id = m.id
WHERE m.tenant_id = @tenant_id
  AND me.tenant_id = @tenant_id
  AND m.id <> @exclude_memory_id
  AND (sqlc.narg('exclude_topic_key')::text IS NULL OR m.topic_key IS DISTINCT FROM sqlc.narg('exclude_topic_key')::text)
  AND m.scope_type = @scope_type::scope_type_enum
  AND (
      (@scope_type::scope_type_enum IN ('org_shared', 'global_public'))
      OR (COALESCE(m.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = COALESCE(sqlc.narg('project_id')::uuid, '00000000-0000-0000-0000-000000000000'::uuid))
  )
  AND (
      (@scope_type::scope_type_enum <> 'agent_private')
      OR (m.owner_agent_id = sqlc.narg('owner_agent_id')::uuid)
  )
  AND (
      (@scope_type::scope_type_enum <> 'task_shared')
      OR (m.task_id = @task_id::text)
  )
  AND m.status IN ('active', 'conflicted')
  AND m.valid_from <= @now
  AND (m.valid_until IS NULL OR m.valid_until > @now)
  AND (me.dense_vec <=> CAST(sqlc.arg('query_vec_text')::text AS halfvec(1024))) <= (1.0 - sqlc.arg('min_similarity')::double precision)
ORDER BY
    (me.dense_vec <=> CAST(sqlc.arg('query_vec_text')::text AS halfvec(1024))) ASC,
    m.observed_at DESC,
    m.id ASC
LIMIT @result_limit;

-- name: ListActiveConflicts :many
SELECT
    r.id AS r_id,
    r.tenant_id AS r_tenant_id,
    r.project_id AS r_project_id,
    r.from_memory_id AS r_from_memory_id,
    r.to_memory_id AS r_to_memory_id,
    r.relation_type AS r_relation_type,
    r.weight AS r_weight,
    r.nli_entailment AS r_nli_entailment,
    r.nli_contradiction AS r_nli_contradiction,
    r.source_id AS r_source_id,
    r.valid_from AS r_valid_from,
    r.valid_until AS r_valid_until,
    r.is_active AS r_is_active,
    r.notes AS r_notes,
    r.created_at AS r_created_at,
    fm.id AS fm_id,
    fm.tenant_id AS fm_tenant_id,
    fm.project_id AS fm_project_id,
    fm.owner_agent_id AS fm_owner_agent_id,
    fm.task_id AS fm_task_id,
    fm.memory_type AS fm_memory_type,
    fm.scope_type AS fm_scope_type,
    fm.status AS fm_status,
    fm.topic_key AS fm_topic_key,
    fm.summary AS fm_summary,
    fm.content AS fm_content,
    fm.content_sha256 AS fm_content_sha256,
    fm.token_count AS fm_token_count,
    fm.tags AS fm_tags,
    fm.entities AS fm_entities,
    fm.epistemic_status AS fm_epistemic_status,
    fm.alpha_score AS fm_alpha_score,
    fm.beta_score AS fm_beta_score,
    fm.confidence AS fm_confidence,
    fm.importance AS fm_importance,
    fm.utility_score AS fm_utility_score,
    fm.access_count AS fm_access_count,
    fm.version AS fm_version,
    fm.nli_checked AS fm_nli_checked,
    fm.primary_source_id AS fm_primary_source_id,
    fm.idempotency_key AS fm_idempotency_key,
    fm.observed_at AS fm_observed_at,
    fm.valid_from AS fm_valid_from,
    fm.valid_until AS fm_valid_until,
    fm.created_at AS fm_created_at,
    fm.updated_at AS fm_updated_at,
    fm.last_accessed_at AS fm_last_accessed_at,
    fm.metadata AS fm_metadata,
    tm.id AS tm_id,
    tm.tenant_id AS tm_tenant_id,
    tm.project_id AS tm_project_id,
    tm.owner_agent_id AS tm_owner_agent_id,
    tm.task_id AS tm_task_id,
    tm.memory_type AS tm_memory_type,
    tm.scope_type AS tm_scope_type,
    tm.status AS tm_status,
    tm.topic_key AS tm_topic_key,
    tm.summary AS tm_summary,
    tm.content AS tm_content,
    tm.content_sha256 AS tm_content_sha256,
    tm.token_count AS tm_token_count,
    tm.tags AS tm_tags,
    tm.entities AS tm_entities,
    tm.epistemic_status AS tm_epistemic_status,
    tm.alpha_score AS tm_alpha_score,
    tm.beta_score AS tm_beta_score,
    tm.confidence AS tm_confidence,
    tm.importance AS tm_importance,
    tm.utility_score AS tm_utility_score,
    tm.access_count AS tm_access_count,
    tm.version AS tm_version,
    tm.nli_checked AS tm_nli_checked,
    tm.primary_source_id AS tm_primary_source_id,
    tm.idempotency_key AS tm_idempotency_key,
    tm.observed_at AS tm_observed_at,
    tm.valid_from AS tm_valid_from,
    tm.valid_until AS tm_valid_until,
    tm.created_at AS tm_created_at,
    tm.updated_at AS tm_updated_at,
    tm.last_accessed_at AS tm_last_accessed_at,
    tm.metadata AS tm_metadata
FROM memory_relations r
JOIN memories fm ON fm.id = r.from_memory_id AND fm.tenant_id = r.tenant_id
JOIN memories tm ON tm.id = r.to_memory_id AND tm.tenant_id = r.tenant_id
WHERE r.tenant_id = @tenant_id
  AND r.relation_type = 'contradicts'
  AND r.is_active = TRUE
  AND r.valid_from <= @now
  AND (r.valid_until IS NULL OR r.valid_until > @now)
  AND (sqlc.narg('project_id')::uuid IS NULL OR r.project_id = sqlc.narg('project_id')::uuid)
  AND fm.status IN ('active', 'conflicted')
  AND tm.status IN ('active', 'conflicted')
ORDER BY r.created_at DESC, r.id ASC
LIMIT @result_limit OFFSET @result_offset;

-- name: GetConflictForUpdate :one
SELECT
    r.id AS r_id,
    r.tenant_id AS r_tenant_id,
    r.project_id AS r_project_id,
    r.from_memory_id AS r_from_memory_id,
    r.to_memory_id AS r_to_memory_id,
    r.relation_type AS r_relation_type,
    r.weight AS r_weight,
    r.nli_entailment AS r_nli_entailment,
    r.nli_contradiction AS r_nli_contradiction,
    r.source_id AS r_source_id,
    r.valid_from AS r_valid_from,
    r.valid_until AS r_valid_until,
    r.is_active AS r_is_active,
    r.notes AS r_notes,
    r.created_at AS r_created_at,
    fm.id AS fm_id,
    fm.tenant_id AS fm_tenant_id,
    fm.project_id AS fm_project_id,
    fm.owner_agent_id AS fm_owner_agent_id,
    fm.task_id AS fm_task_id,
    fm.memory_type AS fm_memory_type,
    fm.scope_type AS fm_scope_type,
    fm.status AS fm_status,
    fm.topic_key AS fm_topic_key,
    fm.summary AS fm_summary,
    fm.content AS fm_content,
    fm.content_sha256 AS fm_content_sha256,
    fm.token_count AS fm_token_count,
    fm.tags AS fm_tags,
    fm.entities AS fm_entities,
    fm.epistemic_status AS fm_epistemic_status,
    fm.alpha_score AS fm_alpha_score,
    fm.beta_score AS fm_beta_score,
    fm.confidence AS fm_confidence,
    fm.importance AS fm_importance,
    fm.utility_score AS fm_utility_score,
    fm.access_count AS fm_access_count,
    fm.version AS fm_version,
    fm.nli_checked AS fm_nli_checked,
    fm.primary_source_id AS fm_primary_source_id,
    fm.idempotency_key AS fm_idempotency_key,
    fm.observed_at AS fm_observed_at,
    fm.valid_from AS fm_valid_from,
    fm.valid_until AS fm_valid_until,
    fm.created_at AS fm_created_at,
    fm.updated_at AS fm_updated_at,
    fm.last_accessed_at AS fm_last_accessed_at,
    fm.metadata AS fm_metadata,
    tm.id AS tm_id,
    tm.tenant_id AS tm_tenant_id,
    tm.project_id AS tm_project_id,
    tm.owner_agent_id AS tm_owner_agent_id,
    tm.task_id AS tm_task_id,
    tm.memory_type AS tm_memory_type,
    tm.scope_type AS tm_scope_type,
    tm.status AS tm_status,
    tm.topic_key AS tm_topic_key,
    tm.summary AS tm_summary,
    tm.content AS tm_content,
    tm.content_sha256 AS tm_content_sha256,
    tm.token_count AS tm_token_count,
    tm.tags AS tm_tags,
    tm.entities AS tm_entities,
    tm.epistemic_status AS tm_epistemic_status,
    tm.alpha_score AS tm_alpha_score,
    tm.beta_score AS tm_beta_score,
    tm.confidence AS tm_confidence,
    tm.importance AS tm_importance,
    tm.utility_score AS tm_utility_score,
    tm.access_count AS tm_access_count,
    tm.version AS tm_version,
    tm.nli_checked AS tm_nli_checked,
    tm.primary_source_id AS tm_primary_source_id,
    tm.idempotency_key AS tm_idempotency_key,
    tm.observed_at AS tm_observed_at,
    tm.valid_from AS tm_valid_from,
    tm.valid_until AS tm_valid_until,
    tm.created_at AS tm_created_at,
    tm.updated_at AS tm_updated_at,
    tm.last_accessed_at AS tm_last_accessed_at,
    tm.metadata AS tm_metadata
FROM memory_relations r
JOIN memories fm ON fm.id = r.from_memory_id AND fm.tenant_id = r.tenant_id
JOIN memories tm ON tm.id = r.to_memory_id AND tm.tenant_id = r.tenant_id
WHERE r.id = @conflict_id
  AND r.tenant_id = @tenant_id
  AND r.relation_type = 'contradicts'
  AND r.is_active = TRUE
FOR UPDATE OF r, fm, tm;

-- name: DeactivateConflictEdge :execrows
UPDATE memory_relations
SET is_active = FALSE,
    valid_until = GREATEST(valid_from, @valid_until::timestamptz)
WHERE id = @conflict_id
  AND tenant_id = @tenant_id
  AND relation_type = 'contradicts'
  AND is_active = TRUE;

-- name: DeactivateConflictsByMemoryID :many
UPDATE memory_relations
SET is_active = FALSE,
    valid_until = GREATEST(valid_from, @valid_until::timestamptz)
WHERE tenant_id = @tenant_id
  AND relation_type = 'contradicts'
  AND is_active = TRUE
  AND (from_memory_id = @memory_id OR to_memory_id = @memory_id)
RETURNING (CASE
    WHEN from_memory_id = @memory_id THEN to_memory_id
    ELSE from_memory_id
END)::uuid AS peer_memory_id;

-- name: HasActiveConflicts :one
SELECT EXISTS (
    SELECT 1
    FROM memory_relations
    WHERE tenant_id = @tenant_id
      AND relation_type = 'contradicts'
      AND is_active = TRUE
      AND valid_from <= @now
      AND (valid_until IS NULL OR valid_until > @now)
      AND (from_memory_id = @memory_id OR to_memory_id = @memory_id)
) AS has_conflicts;
