-- Run only on an isolated test database after migration Up. Leaves no fixtures.
BEGIN;
DO $$
DECLARE
  t uuid := gen_random_uuid();
  s uuid := gen_random_uuid();
  a uuid := gen_random_uuid();
  b uuid := gen_random_uuid();
BEGIN
  INSERT INTO tenants(id,slug,name) VALUES(t,'t001-check','T001');
  INSERT INTO agents(id,tenant_id,external_key,name) VALUES(a,t,'a','A'),(b,t,'b','B');
  INSERT INTO memory_sources(id,tenant_id,source_type,external_source_id)
    VALUES(s,t,'agent_execution','t001');
  INSERT INTO memories(tenant_id,owner_agent_id,memory_type,scope_type,topic_key,summary,content,content_sha256,primary_source_id,idempotency_key)
    VALUES(t,a,'semantic','agent_private','same-topic','test','test',repeat('a',64),s,'once'),
          (t,b,'semantic','agent_private','same-topic','test','test',repeat('a',64),s,NULL);
  BEGIN
    INSERT INTO memories(tenant_id,owner_agent_id,memory_type,scope_type,topic_key,summary,content,content_sha256,primary_source_id)
      VALUES(t,a,'semantic','agent_private','same-topic','test','test',repeat('a',64),s);
    RAISE EXCEPTION 'FAIL: duplicate active topic accepted';
  EXCEPTION WHEN unique_violation THEN NULL;
  END;
  BEGIN
    INSERT INTO memories(tenant_id,memory_type,summary,content,content_sha256,primary_source_id,idempotency_key)
      VALUES(t,'semantic','test','test',repeat('a',64),s,'once');
    RAISE EXCEPTION 'FAIL: duplicate idempotency key accepted';
  EXCEPTION WHEN unique_violation THEN NULL;
  END;
  INSERT INTO memories(tenant_id,task_id,memory_type,scope_type,topic_key,summary,content,content_sha256,primary_source_id)
    VALUES(t,'task-a','semantic','task_shared','same-topic','test','test',repeat('a',64),s),
          (t,'task-b','semantic','task_shared','same-topic','test','test',repeat('a',64),s);
  INSERT INTO audit_events(operation,resource_type,outcome) VALUES('authenticate','principal','denied');
  RAISE NOTICE 'PASS: private scope partition, task partition, active topic uniqueness, idempotency uniqueness, anonymous denied audit';
END $$;
ROLLBACK;
