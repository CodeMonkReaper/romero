$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$container = 'romero-principal-check-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
function Invoke-SQL([string]$SQL, [bool]$ExpectFailure = $false) {
    $result = $SQL | docker exec -i $container psql -U postgres -X -v ON_ERROR_STOP=1 -v VERBOSITY=verbose -1 -At 2>&1
    $code = $LASTEXITCODE
    $result
    if (($code -ne 0) -ne $ExpectFailure) { throw "Unexpected SQL exit code $code (expected failure: $ExpectFailure)" }
    if ($ExpectFailure -and (($result -join "`n") -notmatch '23514:.*chk_principals_allowed_scopes')) {
        throw 'Expected legacy scope check violation (23514), received a different failure'
    }
}
try {
    docker run -d --rm --name $container --network none -e POSTGRES_HOST_AUTH_METHOD=trust pgvector/pgvector:pg17
    if ($LASTEXITCODE -ne 0) { throw 'Could not start isolated PostgreSQL' }
    $ready = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        docker exec $container pg_isready -U postgres *> $null
        if ($LASTEXITCODE -eq 0) { $ready = $true; break }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { throw 'PostgreSQL did not become ready' }
    $base = (Get-Content (Join-Path $repoRoot 'migrations/00001_romero_core_schema.sql') -Raw) -split '-- \+goose Down'
    $patch = (Get-Content (Join-Path $repoRoot 'migrations/00002_principal_authorization.sql') -Raw) -split '-- \+goose Down'
    Invoke-SQL $base[0]
    Invoke-SQL "INSERT INTO tenants(id,slug,name) VALUES('10000000-0000-0000-0000-000000000001','legacy','Legacy'); INSERT INTO principals(tenant_id,principal_type,name,api_key_prefix,api_key_sha256) VALUES('10000000-0000-0000-0000-000000000001','user','legacy','test',repeat('b',64));"
    Invoke-SQL "SELECT allowed_scopes FROM principals;"
    Invoke-SQL $patch[0] $true
    Invoke-SQL @'
DO $$ BEGIN
 IF EXISTS(SELECT FROM information_schema.columns WHERE table_name='principals' AND column_name='is_admin') THEN
   RAISE EXCEPTION 'Failed migration did not roll back';
 END IF;
 IF NOT EXISTS(SELECT FROM principals WHERE allowed_scopes=ARRAY['read','write']) THEN
   RAISE EXCEPTION 'Legacy grants were silently rewritten';
 END IF;
END $$;
'@
    # Only a disposable test fixture is removed, never an application database.
    Invoke-SQL "DELETE FROM tenants WHERE slug='legacy';"
    Invoke-SQL "INSERT INTO tenants(id,slug,name) VALUES('10000000-0000-0000-0000-000000000002','valid','Valid'); INSERT INTO principals(tenant_id,principal_type,name,api_key_prefix,api_key_sha256,allowed_scopes) VALUES('10000000-0000-0000-0000-000000000002','user','valid','test',repeat('c',64),ARRAY['project_shared']);"
    Invoke-SQL $patch[0]
    Invoke-SQL @'
DO $$ BEGIN
 IF NOT EXISTS(SELECT FROM principals WHERE name='valid' AND allowed_scopes=ARRAY['project_shared'] AND is_admin=FALSE) THEN
   RAISE EXCEPTION 'Existing valid principal changed or gained admin';
 END IF;
END $$;
'@
    # The behavior script owns its transaction, so execute it without psql -1.
    Get-Content (Join-Path $PSScriptRoot 'principal-authorization-check.sql') -Raw | docker exec -i $container psql -U postgres -X -v ON_ERROR_STOP=1
    if ($LASTEXITCODE -ne 0) { throw 'Principal behavior checks failed' }
    Get-Content (Join-Path $PSScriptRoot 't001-live-check.sql') -Raw | docker exec -i $container psql -U postgres -X -v ON_ERROR_STOP=1
    if ($LASTEXITCODE -ne 0) { throw 'T001 behavior checks failed' }
    Invoke-SQL $patch[1]
    Invoke-SQL @'
DO $$ BEGIN
 IF EXISTS(SELECT FROM information_schema.columns WHERE table_name='principals' AND column_name='is_admin') THEN
   RAISE EXCEPTION 'Down did not remove is_admin';
 END IF;
END $$;
'@
    Invoke-SQL $patch[0]
    Invoke-SQL $patch[1]
    Invoke-SQL $base[1]
    Invoke-SQL @'
DO $$ BEGIN
 IF EXISTS(SELECT FROM information_schema.tables WHERE table_schema='public') THEN
   RAISE EXCEPTION 'Down left application tables';
 END IF;
END $$;
'@
    'PASS: legacy failure is atomic; grants preserved; new defaults and constraints; Up/Down/Up; full Down'
} finally {
    docker stop $container
}
