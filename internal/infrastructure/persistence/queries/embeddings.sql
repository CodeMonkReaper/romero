-- name: UpsertMemoryEmbedding :exec
INSERT INTO memory_embeddings (
    memory_id,
    tenant_id,
    project_id,
    model_name,
    model_version,
    dimensions,
    dense_vec,
    created_at,
    updated_at
)
SELECT
    m.id,
    m.tenant_id,
    m.project_id,
    @model_name,
    '1.0',
    1024,
    CAST(sqlc.arg('dense_vec_text')::text AS halfvec(1024)),
    @updated_at,
    @updated_at
FROM memories m
WHERE m.id = @memory_id
ON CONFLICT (memory_id) DO UPDATE SET
    model_name = EXCLUDED.model_name,
    dense_vec = EXCLUDED.dense_vec,
    updated_at = EXCLUDED.updated_at;

-- name: GetMemoryEmbeddingsByIDs :many
SELECT
    memory_id,
    dense_vec::text AS dense_vec_text
FROM memory_embeddings
WHERE tenant_id = @tenant_id
  AND memory_id = ANY(@ids::uuid[]);
