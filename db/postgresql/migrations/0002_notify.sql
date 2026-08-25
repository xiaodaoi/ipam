-- 0002_notify.sql — coherence_binding 变更 NOTIFY（§2.3 LISTEN/NOTIFY 触发增量）
CREATE OR REPLACE FUNCTION ipam_notify_coherence() RETURNS trigger AS $$
BEGIN
  IF TG_OP IN ('INSERT','UPDATE') THEN
    PERFORM pg_notify('coherence_change', json_build_object('op','upsert','row',row_to_json(NEW))::text);
  ELSIF TG_OP = 'DELETE' THEN
    PERFORM pg_notify('coherence_change', json_build_object('op','delete','row',row_to_json(OLD))::text);
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_coherence_notify ON coherence_binding;
CREATE TRIGGER trg_coherence_notify
AFTER INSERT OR UPDATE OR DELETE ON coherence_binding
FOR EACH ROW EXECUTE FUNCTION ipam_notify_coherence();
