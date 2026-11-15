-- name: EnqueueMemoryJob :exec
INSERT INTO memory_jobs (
    id,
    tenant_id,
    project_id,
    memory_id,
    job_type,
    priority,
    status,
    payload,
    attempts,
    max_attempts,
    last_error,
    scheduled_at,
    locked_at,
    completed_at,
    created_at
) VALUES (
    @id,
    @tenant_id,
    @project_id,
    @memory_id,
    @job_type,
    @priority,
    @status,
    @payload,
    @attempts,
    @max_attempts,
    NULLIF(@last_error::text, ''),
    @scheduled_at,
    @locked_at,
    @completed_at,
    @created_at
);
