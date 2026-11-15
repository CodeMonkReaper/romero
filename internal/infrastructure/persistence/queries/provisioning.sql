-- name: InsertTenant :one
INSERT INTO tenants (
    id,
    slug,
    name,
    is_active,
    settings
) VALUES (
    @id,
    @slug,
    @name,
    @is_active,
    @settings
)
RETURNING id, slug, name, is_active, settings, created_at, updated_at;

-- name: GetTenantByID :one
SELECT id, slug, name, is_active, settings, created_at, updated_at
FROM tenants
WHERE id = @id;

-- name: GetTenantBySlug :one
SELECT id, slug, name, is_active, settings, created_at, updated_at
FROM tenants
WHERE slug = @slug;

-- name: InsertProject :one
INSERT INTO projects (
    id,
    tenant_id,
    slug,
    name,
    repository_url,
    description,
    is_active,
    metadata
) VALUES (
    @id,
    @tenant_id,
    @slug,
    @name,
    NULLIF(@repository_url::text, ''),
    NULLIF(@description::text, ''),
    @is_active,
    @metadata
)
RETURNING id, tenant_id, slug, name, repository_url, description, is_active, metadata, created_at, updated_at;

-- name: GetProjectByID :one
SELECT id, tenant_id, slug, name, repository_url, description, is_active, metadata, created_at, updated_at
FROM projects
WHERE tenant_id = @tenant_id
  AND id = @id;

-- name: GetProjectBySlug :one
SELECT id, tenant_id, slug, name, repository_url, description, is_active, metadata, created_at, updated_at
FROM projects
WHERE tenant_id = @tenant_id
  AND slug = @slug;

-- name: CountTenantProjectsByIDs :one
SELECT COUNT(*)::bigint
FROM projects
WHERE tenant_id = @tenant_id
  AND id = ANY(@ids::uuid[]);

-- name: InsertAgent :one
INSERT INTO agents (
    id,
    tenant_id,
    external_key,
    name,
    role_type,
    capabilities,
    is_active,
    metadata
) VALUES (
    @id,
    @tenant_id,
    @external_key,
    @name,
    @role_type,
    @capabilities,
    @is_active,
    @metadata
)
RETURNING id, tenant_id, external_key, name, role_type, capabilities, is_active, metadata, created_at, updated_at;

-- name: GetAgentByID :one
SELECT id, tenant_id, external_key, name, role_type, capabilities, is_active, metadata, created_at, updated_at
FROM agents
WHERE tenant_id = @tenant_id
  AND id = @id;

-- name: GetAgentByExternalKey :one
SELECT id, tenant_id, external_key, name, role_type, capabilities, is_active, metadata, created_at, updated_at
FROM agents
WHERE tenant_id = @tenant_id
  AND external_key = @external_key;

-- name: InsertPrincipal :one
INSERT INTO principals (
    id,
    tenant_id,
    agent_id,
    principal_type,
    name,
    api_key_prefix,
    api_key_sha256,
    allowed_projects,
    allowed_scopes,
    is_active,
    expires_at,
    is_admin
) VALUES (
    @id,
    @tenant_id,
    @agent_id,
    @principal_type,
    @name,
    @api_key_prefix,
    @api_key_sha256,
    @allowed_projects,
    @allowed_scopes,
    @is_active,
    @expires_at,
    @is_admin
)
RETURNING id, tenant_id, agent_id, principal_type, name, api_key_prefix, allowed_projects, allowed_scopes, is_active, expires_at, created_at, is_admin;

-- name: GetPrincipalByID :one
SELECT id, tenant_id, agent_id, principal_type, name, api_key_prefix, allowed_projects, allowed_scopes, is_active, expires_at, created_at, is_admin
FROM principals
WHERE tenant_id = @tenant_id
  AND id = @id;

-- name: LookupPrincipalsByPrefix :many
SELECT
    p.id,
    p.tenant_id,
    p.agent_id,
    p.principal_type,
    p.name,
    p.api_key_prefix,
    p.api_key_sha256,
    p.allowed_projects,
    p.allowed_scopes,
    p.is_active,
    p.expires_at,
    p.created_at,
    p.is_admin,
    t.is_active AS tenant_active,
    (p.agent_id IS NULL OR COALESCE(a.is_active, FALSE))::boolean AS agent_active
FROM principals p
JOIN tenants t
  ON t.id = p.tenant_id
LEFT JOIN agents a
  ON a.id = p.agent_id
 AND a.tenant_id = p.tenant_id
WHERE p.api_key_prefix = @api_key_prefix;
