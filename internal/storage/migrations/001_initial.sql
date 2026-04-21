CREATE TABLE IF NOT EXISTS request_logs (
    id                      TEXT PRIMARY KEY,
    timestamp               TIMESTAMP NOT NULL,
    merchant_key            TEXT NOT NULL,
    region                  TEXT NOT NULL,
    method                  TEXT NOT NULL,
    path                    TEXT NOT NULL,
    query_params            TEXT,
    status_code             INTEGER NOT NULL,
    request_content_length  INTEGER DEFAULT 0,
    response_content_length INTEGER DEFAULT 0,
    cache_status            TEXT,
    queued                  BOOLEAN DEFAULT FALSE,
    queue_wait_ms           INTEGER DEFAULT 0,
    upstream_latency_ms     INTEGER,
    total_latency_ms        INTEGER,
    pii_redacted            BOOLEAN DEFAULT FALSE,
    amazon_request_id       TEXT,
    body_file               TEXT,
    body_offset             INTEGER,
    body_length             INTEGER
);

CREATE INDEX idx_logs_merchant_time ON request_logs(merchant_key, timestamp DESC);
CREATE INDEX idx_logs_status ON request_logs(status_code);
CREATE INDEX idx_logs_path ON request_logs(path);
CREATE INDEX idx_logs_cache ON request_logs(cache_status);
