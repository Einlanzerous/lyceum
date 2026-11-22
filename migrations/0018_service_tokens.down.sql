-- Drops the service-token table and the audit trail. The trail is lost with it.
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS service_tokens;
