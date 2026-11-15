-- name: SearchMemoriesByTrigram :many
SELECT m.id
FROM memories m
WHERE m.tenant_id = @tenant_id
  AND (
      (m.scope_type = 'global_public' AND 'global_public' = ANY(@scopes::text[]))
      OR (m.scope_type = 'org_shared' AND 'org_shared' = ANY(@scopes::text[]))
      OR (m.scope_type = 'project_shared' AND 'project_shared' = ANY(@scopes::text[]) AND m.project_id = ANY(@project_ids::uuid[]))
      OR (m.scope_type = 'task_shared' AND 'task_shared' = ANY(@scopes::text[]) AND m.project_id = ANY(@project_ids::uuid[]) AND m.task_id = ANY(@allowed_tasks::text[]))
      OR (m.scope_type = 'agent_private' AND 'agent_private' = ANY(@scopes::text[]) AND m.owner_agent_id = @caller_agent_id AND (m.project_id IS NULL OR m.project_id = ANY(@project_ids::uuid[])))
  )
  AND (
      m.status = 'active'
      OR m.status = 'conflicted'
      OR (@include_superseded::boolean AND m.status = 'superseded')
      OR (@include_consolidated::boolean AND m.status = 'consolidated')
  )
  AND (
      m.status = 'superseded'
      OR (m.valid_from <= @now AND (m.valid_until IS NULL OR m.valid_until > @now))
  )
  AND (
      m.summary % @query_text
      OR m.topic_key % @query_text
      OR m.summary ILIKE '%' || @query_text || '%'
  )
ORDER BY
    GREATEST(similarity(m.summary, @query_text), COALESCE(similarity(m.topic_key, @query_text), 0)) DESC,
    m.observed_at DESC,
    m.id ASC
LIMIT @result_limit;

-- name: SearchMemoriesByFTS :many
SELECT m.id
FROM memories m
WHERE m.tenant_id = @tenant_id
  AND (
      (m.scope_type = 'global_public' AND 'global_public' = ANY(@scopes::text[]))
      OR (m.scope_type = 'org_shared' AND 'org_shared' = ANY(@scopes::text[]))
      OR (m.scope_type = 'project_shared' AND 'project_shared' = ANY(@scopes::text[]) AND m.project_id = ANY(@project_ids::uuid[]))
      OR (m.scope_type = 'task_shared' AND 'task_shared' = ANY(@scopes::text[]) AND m.project_id = ANY(@project_ids::uuid[]) AND m.task_id = ANY(@allowed_tasks::text[]))
      OR (m.scope_type = 'agent_private' AND 'agent_private' = ANY(@scopes::text[]) AND m.owner_agent_id = @caller_agent_id AND (m.project_id IS NULL OR m.project_id = ANY(@project_ids::uuid[])))
  )
  AND (
      m.status = 'active'
      OR m.status = 'conflicted'
      OR (@include_superseded::boolean AND m.status = 'superseded')
      OR (@include_consolidated::boolean AND m.status = 'consolidated')
  )
  AND (
      m.status = 'superseded'
      OR (m.valid_from <= @now AND (m.valid_until IS NULL OR m.valid_until > @now))
  )
  AND (
      m.fts_doc @@ websearch_to_tsquery('spanish', @query_text)
      OR m.fts_doc @@ websearch_to_tsquery('simple', @query_text)
  )
ORDER BY
    (
        ts_rank_cd(m.fts_doc, websearch_to_tsquery('spanish', @query_text)) +
        ts_rank_cd(m.fts_doc, websearch_to_tsquery('simple', @query_text))
    ) DESC,
    m.observed_at DESC,
    m.id ASC
LIMIT @result_limit;

-- name: SearchMemoriesByVector :many
SELECT m.id
FROM memories m
JOIN memory_embeddings me ON me.memory_id = m.id
WHERE m.tenant_id = @tenant_id
  AND me.tenant_id = @tenant_id
  AND (
      (m.scope_type = 'global_public' AND 'global_public' = ANY(@scopes::text[]))
      OR (m.scope_type = 'org_shared' AND 'org_shared' = ANY(@scopes::text[]))
      OR (m.scope_type = 'project_shared' AND 'project_shared' = ANY(@scopes::text[]) AND m.project_id = ANY(@project_ids::uuid[]))
      OR (m.scope_type = 'task_shared' AND 'task_shared' = ANY(@scopes::text[]) AND m.project_id = ANY(@project_ids::uuid[]) AND m.task_id = ANY(@allowed_tasks::text[]))
      OR (m.scope_type = 'agent_private' AND 'agent_private' = ANY(@scopes::text[]) AND m.owner_agent_id = @caller_agent_id AND (m.project_id IS NULL OR m.project_id = ANY(@project_ids::uuid[])))
  )
  AND (
      m.status = 'active'
      OR m.status = 'conflicted'
      OR (@include_superseded::boolean AND m.status = 'superseded')
      OR (@include_consolidated::boolean AND m.status = 'consolidated')
  )
  AND (
      m.status = 'superseded'
      OR (m.valid_from <= @now AND (m.valid_until IS NULL OR m.valid_until > @now))
  )
ORDER BY
    (me.dense_vec <=> CAST(sqlc.arg('query_vec_text')::text AS halfvec(1024))) ASC,
    m.observed_at DESC,
    m.id ASC
LIMIT @result_limit;

-- name: GetMemoriesByIDs :many
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
  AND id = ANY(@ids::uuid[]);

-- name: LoadProjectGraphMemories :many
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
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id IS NULL OR project_id = sqlc.narg('project_id')::uuid)
  AND status IN ('active', 'conflicted', 'consolidated', 'superseded')
  AND (
      status = 'superseded'
      OR (valid_from <= @now AND (valid_until IS NULL OR valid_until > @now))
  );

-- name: LoadProjectGraphRelations :many
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
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id IS NULL OR project_id = sqlc.narg('project_id')::uuid)
  AND is_active = TRUE
  AND valid_from <= @now
  AND (valid_until IS NULL OR valid_until > @now);

-- name: LoadProjectGraphEmbeddings :many
SELECT
    memory_id,
    dense_vec::text AS dense_vec_text
FROM memory_embeddings
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id IS NULL OR project_id = sqlc.narg('project_id')::uuid);

-- name: TraverseCausalSubgraph :many
WITH hop1 AS (
    SELECT
        r.id,
        r.from_memory_id,
        r.to_memory_id,
        r.relation_type,
        r.weight,
        1 AS depth
    FROM memory_relations r
    WHERE r.tenant_id = @tenant_id
      AND r.is_active = TRUE
      AND r.valid_from <= @now
      AND (r.valid_until IS NULL OR r.valid_until > @now)
      AND (r.from_memory_id = ANY(@seeds::uuid[]) OR r.to_memory_id = ANY(@seeds::uuid[]))
),
hop2 AS (
    SELECT
        r.id,
        r.from_memory_id,
        r.to_memory_id,
        r.relation_type,
        r.weight,
        2 AS depth
    FROM memory_relations r
    WHERE sqlc.arg('max_hops')::int >= 2
      AND r.tenant_id = @tenant_id
      AND r.is_active = TRUE
      AND r.valid_from <= @now
      AND (r.valid_until IS NULL OR r.valid_until > @now)
      AND (
          r.from_memory_id IN (SELECT from_memory_id FROM hop1 UNION SELECT to_memory_id FROM hop1)
          OR r.to_memory_id IN (SELECT from_memory_id FROM hop1 UNION SELECT to_memory_id FROM hop1)
      )
),
hop3 AS (
    SELECT
        r.id,
        r.from_memory_id,
        r.to_memory_id,
        r.relation_type,
        r.weight,
        3 AS depth
    FROM memory_relations r
    WHERE sqlc.arg('max_hops')::int >= 3
      AND r.tenant_id = @tenant_id
      AND r.is_active = TRUE
      AND r.valid_from <= @now
      AND (r.valid_until IS NULL OR r.valid_until > @now)
      AND (
          r.from_memory_id IN (SELECT from_memory_id FROM hop2 UNION SELECT to_memory_id FROM hop2)
          OR r.to_memory_id IN (SELECT from_memory_id FROM hop2 UNION SELECT to_memory_id FROM hop2)
      )
),
all_hops AS (
    SELECT from_memory_id, to_memory_id, relation_type, weight, depth FROM hop1
    UNION ALL
    SELECT from_memory_id, to_memory_id, relation_type, weight, depth FROM hop2
    UNION ALL
    SELECT from_memory_id, to_memory_id, relation_type, weight, depth FROM hop3
)
SELECT
    from_memory_id,
    to_memory_id,
    relation_type,
    weight,
    MIN(depth)::int AS depth
FROM all_hops
GROUP BY from_memory_id, to_memory_id, relation_type, weight;
