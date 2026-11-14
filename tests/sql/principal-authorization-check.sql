-- Run after both migrations on an isolated PostgreSQL database.
BEGIN;
DO $$
DECLARE
  t uuid := gen_random_uuid();
  p uuid;
  admin boolean;
  scopes text[];
BEGIN
  INSERT INTO tenants(id,slug,name) VALUES(t,'principal-check','Check');
  INSERT INTO principals(tenant_id,principal_type,name,api_key_prefix,api_key_sha256)
    VALUES(t,'user','default','test',repeat('a',64))
    RETURNING id,is_admin,allowed_scopes INTO p,admin,scopes;
  IF admin IS DISTINCT FROM FALSE OR scopes IS DISTINCT FROM '{}'::text[] THEN
    RAISE EXCEPTION 'FAIL: default principal has permissions';
  END IF;
  BEGIN
    UPDATE principals SET allowed_scopes=ARRAY['read','write'] WHERE id=p;
    RAISE EXCEPTION 'FAIL: invalid scopes accepted';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
  BEGIN
    UPDATE principals SET allowed_scopes=ARRAY['global_public',NULL] WHERE id=p;
    RAISE EXCEPTION 'FAIL: NULL scope accepted';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
  UPDATE principals SET is_admin=TRUE,
    allowed_scopes=ARRAY['agent_private','task_shared','project_shared','org_shared','global_public']
    WHERE id=p;
  SELECT is_admin,allowed_scopes INTO admin,scopes FROM principals WHERE id=p;
  IF admin IS DISTINCT FROM TRUE OR cardinality(scopes) <> 5 THEN
    RAISE EXCEPTION 'FAIL: explicit administrative grants not persisted';
  END IF;
  RAISE NOTICE 'PASS: deny-by-default principal, invalid/NULL scope rejection, explicit grants persist';
END $$;
ROLLBACK;
