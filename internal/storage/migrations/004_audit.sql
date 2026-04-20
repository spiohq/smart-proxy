CREATE TABLE IF NOT EXISTS audit_log (
    id          TEXT PRIMARY KEY,
    timestamp   TIMESTAMP NOT NULL,
    event_type  TEXT NOT NULL,
    source      TEXT NOT NULL,
    message     TEXT NOT NULL,
    metadata    TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_log_event_type ON audit_log(event_type);
