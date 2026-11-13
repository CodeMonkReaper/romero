-- +goose Up
-- +goose StatementBegin

-- 1. EXTENSIONES REQUERIDAS
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";      -- pgvector 0.8+ (soporte halfvec, sparsevec y HNSW)
CREATE EXTENSION IF NOT EXISTS "pg_trgm";     -- Índices de trigramas para código exacto y rutas
CREATE EXTENSION IF NOT EXISTS "btree_gist";  -- Restricciones temporales e índices compuestos

-- 2. TIPOS ENUMERADOS DEL DOMINIO COGNITIVO
CREATE TYPE memory_type_enum AS ENUM (
    'episodic',
    'semantic',
    'procedural',
    'relational'
);

CREATE TYPE memory_status_enum AS ENUM (
    'candidate',
    'active',
    'superseded',
    'conflicted',
    'consolidated',
    'archived',
    'rejected'
);

CREATE TYPE epistemic_status_enum AS ENUM (
    'observed',
    'reported',
    'inferred',
    'hypothesis',
    'verified'
);

CREATE TYPE scope_type_enum AS ENUM (
    'agent_private',
    'task_shared',
    'project_shared',
    'org_shared',
    'global_public'
);

CREATE TYPE source_type_enum AS ENUM (
    'agent_execution',
    'user_input',
    'tool_output',
    'document_ingest',
    'rem_consolidation',
    'human_review'
);

CREATE TYPE relation_type_enum AS ENUM (
    'supports',
    'contradicts',
    'supersedes',
    'caused_by',
    'depends_on',
    'derived_from',
    'related_to'
);

CREATE TYPE feedback_signal_enum AS ENUM (
    'retrieved',
    'used_in_context',
    'helpful_success',
    'ignored',
    'misleading_failure',
    'corrected_by_user',
    'confirmed_verified'
);

CREATE TYPE job_status_enum AS ENUM (
    'pending',
    'processing',
    'completed',
    'failed',
    'dead_letter'
);

-- 3. IDENTIDAD, TENANTS, PROYECTOS Y AGENTES
CREATE TABLE tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            VARCHAR(64) NOT NULL UNIQUE,
    name            VARCHAR(255) NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    settings        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE projects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slug            VARCHAR(100) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    repository_url  TEXT,
    description     TEXT,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, slug)
);

CREATE INDEX idx_projects_tenant ON projects(tenant_id);

CREATE TABLE agents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    external_key    VARCHAR(128) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    role_type       VARCHAR(64) NOT NULL DEFAULT 'general',
    capabilities    TEXT[] NOT NULL DEFAULT '{}',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, external_key)
);

CREATE INDEX idx_agents_tenant ON agents(tenant_id);

CREATE TABLE principals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id            UUID REFERENCES agents(id) ON DELETE SET NULL,
    principal_type      VARCHAR(32) NOT NULL CHECK (principal_type IN ('user', 'service', 'orchestrator', 'agent')),
    name                VARCHAR(255) NOT NULL,
    api_key_prefix      VARCHAR(24) NOT NULL,
    api_key_sha256      CHAR(64) NOT NULL UNIQUE,
    allowed_projects    UUID[] DEFAULT NULL,
    allowed_scopes      TEXT[] NOT NULL DEFAULT ARRAY['read', 'write'],
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_principals_lookup ON principals(api_key_prefix, is_active);

-- 4. PROCEDENCIA INMUTABLE (MEMORY_SOURCES)
CREATE TABLE memory_sources (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    source_type         source_type_enum NOT NULL,
    external_source_id  VARCHAR(255) NOT NULL,
    actor_principal_id  UUID REFERENCES principals(id) ON DELETE SET NULL,
    actor_agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    uri                 TEXT,
    raw_excerpt_hash    CHAR(64),
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_sources_lookup ON memory_sources(tenant_id, source_type, external_source_id);

-- 5. TABLA CENTRAL DE RECUERDOS (MEMORIES)
CREATE TABLE memories (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    owner_agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    task_id             VARCHAR(128),

    -- Clasificación Cognitiva y Visibilidad
    memory_type         memory_type_enum NOT NULL,
    scope_type          scope_type_enum NOT NULL DEFAULT 'project_shared',
    status              memory_status_enum NOT NULL DEFAULT 'active',
    topic_key           VARCHAR(255),

    -- Contenido (Progressive Disclosure: summary + content)
    summary             VARCHAR(600) NOT NULL,
    content             TEXT NOT NULL,
    content_sha256      CHAR(64) NOT NULL,
    token_count         INTEGER NOT NULL DEFAULT 50,
    tags                TEXT[] NOT NULL DEFAULT '{}',
    entities            TEXT[] NOT NULL DEFAULT '{}',

    -- Epistemología y Calibración Bayesiana Beta(alpha, beta)
    epistemic_status    epistemic_status_enum NOT NULL DEFAULT 'observed',
    alpha_score         REAL NOT NULL DEFAULT 8.50 CHECK (alpha_score > 0.0),
    beta_score          REAL NOT NULL DEFAULT 1.50 CHECK (beta_score > 0.0),
    confidence          REAL GENERATED ALWAYS AS (alpha_score / (alpha_score + beta_score)) STORED,
    importance          REAL NOT NULL DEFAULT 0.50 CHECK (importance >= 0.0 AND importance <= 1.0),
    utility_score       REAL NOT NULL DEFAULT 0.00 CHECK (utility_score >= -1.0 AND utility_score <= 1.0),
    access_count        INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    nli_checked         BOOLEAN NOT NULL DEFAULT FALSE,

    -- Procedencia e Idempotencia
    primary_source_id   UUID NOT NULL REFERENCES memory_sources(id) ON DELETE RESTRICT,
    idempotency_key     VARCHAR(128),

    -- Los Tres Relojes Temporales (Modelo Tri-Temporal)
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_accessed_at    TIMESTAMPTZ,

    -- Metadatos y Documento Full-Text Ponderado (Español + Código Simple)
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    fts_doc             TSVECTOR GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(topic_key, '')), 'A') ||
        setweight(to_tsvector('spanish', coalesce(summary, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(summary, '')), 'A') ||
        setweight(to_tsvector('spanish', coalesce(content, '')), 'B')
    ) STORED,

    CONSTRAINT chk_valid_temporal_window CHECK (valid_until IS NULL OR valid_until >= valid_from)
);

CREATE UNIQUE INDEX idx_memories_idempotency
    ON memories(tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX idx_memories_active_topic_key
    ON memories(
        tenant_id,
        COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid),
        scope_type,
        (CASE WHEN scope_type = 'agent_private' THEN COALESCE(owner_agent_id, '00000000-0000-0000-0000-000000000000'::uuid) ELSE '00000000-0000-0000-0000-000000000000'::uuid END),
        (CASE WHEN scope_type = 'task_shared' THEN COALESCE(task_id, '') ELSE '' END),
        topic_key
    )
    WHERE status = 'active' AND topic_key IS NOT NULL;

CREATE INDEX idx_memories_scope_lookup
    ON memories(tenant_id, project_id, scope_type, status, memory_type);

CREATE INDEX idx_memories_bitemporal
    ON memories(tenant_id, status, valid_from, valid_until, observed_at DESC);

CREATE INDEX idx_memories_rem_candidates
    ON memories(tenant_id, project_id, memory_type, status, created_at)
    WHERE memory_type = 'episodic' AND status = 'active';

CREATE INDEX idx_memories_fts ON memories USING GIN (fts_doc);
CREATE INDEX idx_memories_trgm_summary ON memories USING GIN (summary gin_trgm_ops);
CREATE INDEX idx_memories_trgm_topic ON memories USING GIN (topic_key gin_trgm_ops) WHERE topic_key IS NOT NULL;
CREATE INDEX idx_memories_tags ON memories USING GIN (tags);
CREATE INDEX idx_memories_entities ON memories USING GIN (entities);

-- 6. HISTORIAL DE VERSIONES INMUTABLE (AUTOCONTENIDO PARA CONSULTAS POINT-IN-TIME)
CREATE TABLE memory_versions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_id           UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    version             INTEGER NOT NULL,
    summary             VARCHAR(600) NOT NULL,
    content             TEXT NOT NULL,
    tags                TEXT[] NOT NULL DEFAULT '{}',
    entities            TEXT[] NOT NULL DEFAULT '{}',
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    epistemic_status    epistemic_status_enum NOT NULL,
    alpha_score         REAL NOT NULL CHECK (alpha_score > 0.0),
    beta_score          REAL NOT NULL CHECK (beta_score > 0.0),
    importance          REAL NOT NULL DEFAULT 0.50 CHECK (importance >= 0.0 AND importance <= 1.0),
    utility_score       REAL NOT NULL DEFAULT 0.00 CHECK (utility_score >= -1.0 AND utility_score <= 1.0),
    status              memory_status_enum NOT NULL,
    valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until         TIMESTAMPTZ,
    changed_by_source   UUID REFERENCES memory_sources(id) ON DELETE SET NULL,
    change_reason       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_version_temporal_window CHECK (valid_until IS NULL OR valid_until >= valid_from),
    UNIQUE (memory_id, version)
);

CREATE INDEX idx_memory_versions_lookup ON memory_versions(memory_id, version DESC);
CREATE INDEX idx_memory_versions_pit ON memory_versions(memory_id, created_at DESC, valid_from, valid_until);

-- 7. EMBEDDINGS MULTIMODALES (DENSOS EN HALFVEC FP16 + DISPERSOS SPLADE OPCIONALES)
CREATE TABLE memory_embeddings (
    memory_id           UUID PRIMARY KEY REFERENCES memories(id) ON DELETE CASCADE,
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    model_name          VARCHAR(128) NOT NULL DEFAULT 'BAAI/bge-m3',
    model_version       VARCHAR(64) NOT NULL DEFAULT '1.0',
    dimensions          INTEGER NOT NULL DEFAULT 1024,
    dense_vec           halfvec(1024) NOT NULL,
    sparse_vec          sparsevec(250002),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_embeddings_hnsw_dense
    ON memory_embeddings
    USING hnsw (dense_vec halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64);

CREATE INDEX idx_memory_embeddings_tenant_proj ON memory_embeddings(tenant_id, project_id);

-- 8. GRAFO BITEMPORAL DE RELACIONES CAUSALES Y LÓGICAS (MEMORY_RELATIONS)
CREATE TABLE memory_relations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    from_memory_id      UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    to_memory_id        UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    relation_type       relation_type_enum NOT NULL,
    weight              REAL NOT NULL DEFAULT 1.0 CHECK (weight >= 0.0 AND weight <= 1.0),
    nli_entailment      REAL,
    nli_contradiction   REAL,
    source_id           UUID REFERENCES memory_sources(id) ON DELETE SET NULL,
    valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until         TIMESTAMPTZ,
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    notes               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_no_self_loop CHECK (from_memory_id <> to_memory_id),
    CONSTRAINT chk_relation_temporal_window CHECK (valid_until IS NULL OR valid_until >= valid_from),
    UNIQUE (tenant_id, from_memory_id, to_memory_id, relation_type)
);

CREATE INDEX idx_memory_relations_forward
    ON memory_relations(tenant_id, project_id, from_memory_id, is_active)
    INCLUDE (to_memory_id, relation_type, weight);

CREATE INDEX idx_memory_relations_backward
    ON memory_relations(tenant_id, project_id, to_memory_id, is_active)
    INCLUDE (from_memory_id, relation_type, weight);

-- 9. SEÑALES DE FEEDBACK Y RECALIBRACIÓN BAYESIANA
CREATE TABLE memory_feedback (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    memory_id           UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    agent_id            UUID REFERENCES agents(id) ON DELETE SET NULL,
    task_id             VARCHAR(128),
    signal              feedback_signal_enum NOT NULL,
    alpha_delta         REAL NOT NULL DEFAULT 0.0,
    beta_delta          REAL NOT NULL DEFAULT 0.0,
    utility_delta       REAL NOT NULL DEFAULT 0.0,
    comment             TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_feedback_lookup ON memory_feedback(memory_id, created_at DESC);

-- 10. COLA ASÍNCRONA CON PRIORIDADES (REAL-TIME vs. SUEÑO REM)
CREATE TABLE memory_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    memory_id           UUID REFERENCES memories(id) ON DELETE CASCADE,
    job_type            VARCHAR(64) NOT NULL,
    priority            SMALLINT NOT NULL DEFAULT 10,
    status              job_status_enum NOT NULL DEFAULT 'pending',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempts            INTEGER NOT NULL DEFAULT 0,
    max_attempts        INTEGER NOT NULL DEFAULT 5,
    last_error          TEXT,
    scheduled_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at           TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_jobs_dequeue
    ON memory_jobs(status, priority DESC, scheduled_at ASC)
    WHERE status = 'pending';

-- 11. REGISTRO INMUTABLE DE AUDITORÍA Y EJECUCIONES DE CONSOLIDACIÓN
CREATE TABLE audit_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID REFERENCES tenants(id) ON DELETE CASCADE,
    principal_id        UUID REFERENCES principals(id) ON DELETE SET NULL,
    agent_id            UUID REFERENCES agents(id) ON DELETE SET NULL,
    operation           VARCHAR(64) NOT NULL,
    resource_type       VARCHAR(64) NOT NULL,
    resource_id         UUID,
    scope_used          VARCHAR(64),
    correlation_id      VARCHAR(128),
    outcome             VARCHAR(32) NOT NULL CHECK (outcome IN ('allowed', 'denied')),
    details             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_events_tenant_time ON audit_events(tenant_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS memory_jobs;
DROP TABLE IF EXISTS memory_feedback;
DROP TABLE IF EXISTS memory_relations;
DROP TABLE IF EXISTS memory_embeddings;
DROP TABLE IF EXISTS memory_versions;
DROP TABLE IF EXISTS memories;
DROP TABLE IF EXISTS memory_sources;
DROP TABLE IF EXISTS principals;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS tenants;
DROP TYPE IF EXISTS job_status_enum;
DROP TYPE IF EXISTS feedback_signal_enum;
DROP TYPE IF EXISTS relation_type_enum;
DROP TYPE IF EXISTS source_type_enum;
DROP TYPE IF EXISTS scope_type_enum;
DROP TYPE IF EXISTS epistemic_status_enum;
DROP TYPE IF EXISTS memory_status_enum;
DROP TYPE IF EXISTS memory_type_enum;
-- +goose StatementEnd
