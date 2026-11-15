-- name: InsertOpaqueRequest :exec
INSERT INTO opaque_memory_requests (
    request_id,
    tenant_id,
    project_id,
    task_id,
    principal_id,
    operation,
    target_memory_id,
    payload_version,
    payload,
    request_sha256,
    idempotency_key,
    max_retries
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11,
    $12
);

-- name: GetOpaqueRequestByIdempotencyKey :one
SELECT
    request_id,
    tenant_id,
    project_id,
    task_id,
    principal_id,
    operation,
    target_memory_id,
    payload_version,
    payload,
    request_sha256,
    idempotency_key,
    status,
    attempt_count,
    max_retries,
    next_attempt_at,
    lease_token,
    lease_expires_at,
    last_error_code,
    last_error_message,
    result_memory_id,
    created_at,
    updated_at,
    completed_at
FROM opaque_memory_requests
WHERE tenant_id = $1
  AND principal_id = $2
  AND idempotency_key = $3;

-- name: GetOpaqueRequestByID :one
SELECT
    request_id,
    tenant_id,
    project_id,
    task_id,
    principal_id,
    operation,
    target_memory_id,
    payload_version,
    payload,
    request_sha256,
    idempotency_key,
    status,
    attempt_count,
    max_retries,
    next_attempt_at,
    lease_token,
    lease_expires_at,
    last_error_code,
    last_error_message,
    result_memory_id,
    created_at,
    updated_at,
    completed_at
FROM opaque_memory_requests
WHERE request_id = $1
  AND tenant_id = $2;

-- name: GetOpaqueRequestForExecution :one
SELECT
    request_id,
    tenant_id,
    project_id,
    task_id,
    principal_id,
    operation,
    target_memory_id,
    payload_version,
    payload,
    request_sha256,
    idempotency_key,
    status,
    attempt_count,
    max_retries,
    next_attempt_at,
    lease_token,
    lease_expires_at,
    last_error_code,
    last_error_message,
    result_memory_id,
    created_at,
    updated_at,
    completed_at
FROM opaque_memory_requests
WHERE request_id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > NOW();

-- name: ClaimNextOpaqueRequest :one
WITH candidate AS (
    SELECT request_id
    FROM opaque_memory_requests
    WHERE (status = 'pending' AND next_attempt_at <= NOW())
       OR (status = 'running' AND lease_expires_at <= NOW())
    ORDER BY
        CASE WHEN status = 'running' THEN lease_expires_at ELSE next_attempt_at END ASC,
        created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE opaque_memory_requests AS r
SET
    status = 'running',
    lease_token = $1,
    lease_expires_at = NOW() + make_interval(secs => sqlc.arg(lease_seconds)::integer),
    updated_at = NOW()
FROM candidate
WHERE r.request_id = candidate.request_id
RETURNING
    r.request_id,
    r.tenant_id,
    r.project_id,
    r.task_id,
    r.principal_id,
    r.operation,
    r.target_memory_id,
    r.payload_version,
    r.payload,
    r.request_sha256,
    r.idempotency_key,
    r.status,
    r.attempt_count,
    r.max_retries,
    r.next_attempt_at,
    r.lease_token,
    r.lease_expires_at,
    r.created_at,
    r.updated_at;

-- name: RenewOpaqueRequestLease :execrows
UPDATE opaque_memory_requests
SET
    lease_expires_at = NOW() + make_interval(secs => sqlc.arg(lease_seconds)::integer),
    updated_at = NOW()
WHERE request_id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > NOW();

-- name: CompleteOpaqueRequest :execrows
UPDATE opaque_memory_requests
SET
    status = 'completed',
    lease_token = NULL,
    lease_expires_at = NULL,
    last_error_code = NULL,
    last_error_message = NULL,
    result_memory_id = $3,
    updated_at = NOW(),
    completed_at = NOW()
WHERE request_id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > NOW();

-- name: ScheduleOpaqueRequestRetry :execrows
UPDATE opaque_memory_requests
SET
    status = 'pending',
    attempt_count = attempt_count + 1,
    next_attempt_at = NOW() + make_interval(secs => sqlc.arg(backoff_seconds)::integer),
    lease_token = NULL,
    lease_expires_at = NULL,
    last_error_code = $3,
    last_error_message = $4,
    updated_at = NOW()
WHERE request_id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > NOW()
  AND attempt_count < max_retries;

-- name: MarkOpaqueRequestFailedPermanent :execrows
UPDATE opaque_memory_requests
SET
    status = 'failed',
    attempt_count = attempt_count + 1,
    lease_token = NULL,
    lease_expires_at = NULL,
    last_error_code = $3,
    last_error_message = $4,
    updated_at = NOW(),
    completed_at = NOW()
WHERE request_id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > NOW();

-- name: PurgeTerminalOpaqueRequests :execrows
DELETE FROM opaque_memory_requests
WHERE status IN ('completed', 'failed')
  AND completed_at <= NOW() - make_interval(secs => sqlc.arg(retention_seconds)::double precision);
