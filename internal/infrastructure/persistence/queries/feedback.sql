-- name: InsertMemoryFeedback :exec
INSERT INTO memory_feedback (
    id,
    tenant_id,
    memory_id,
    agent_id,
    task_id,
    signal,
    alpha_delta,
    beta_delta,
    utility_delta,
    comment,
    created_at
) VALUES (
    @id,
    @tenant_id,
    @memory_id,
    @agent_id,
    NULLIF(@task_id::text, ''),
    @signal,
    @alpha_delta,
    @beta_delta,
    @utility_delta,
    NULLIF(@comment::text, ''),
    @created_at
);
