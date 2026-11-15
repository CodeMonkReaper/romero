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

-- name: ClaimPendingMemoryJobs :many
WITH claimed AS (
    SELECT mj.id
    FROM memory_jobs mj
    WHERE mj.job_type = sqlc.arg('job_type')
      AND mj.status IN ('pending', 'failed')
      AND mj.scheduled_at <= sqlc.arg('now_ts')
      AND mj.attempts < mj.max_attempts
    ORDER BY mj.priority DESC, mj.scheduled_at ASC, mj.created_at ASC
    LIMIT sqlc.arg('batch_limit')
    FOR UPDATE SKIP LOCKED
)
UPDATE memory_jobs j
SET status = 'processing',
    attempts = j.attempts + 1,
    locked_at = sqlc.arg('now_ts')
FROM claimed
WHERE j.id = claimed.id
RETURNING
    j.id,
    j.tenant_id,
    j.project_id,
    j.memory_id,
    j.job_type,
    j.priority,
    j.status,
    j.payload,
    j.attempts,
    j.max_attempts,
    j.last_error,
    j.scheduled_at,
    j.locked_at,
    j.completed_at,
    j.created_at;

-- name: CompleteMemoryJob :exec
UPDATE memory_jobs
SET status = 'completed',
    completed_at = @completed_at,
    locked_at = NULL
WHERE id = @id;

-- name: FailMemoryJob :exec
UPDATE memory_jobs
SET status = CASE WHEN @dead_letter::boolean THEN 'dead_letter'::job_status_enum ELSE 'failed'::job_status_enum END,
    last_error = NULLIF(@last_error::text, ''),
    scheduled_at = @next_scheduled_at,
    locked_at = NULL
WHERE id = @id;
