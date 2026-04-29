ALTER TABLE request_logs ADD COLUMN pii_redacted_request BOOLEAN DEFAULT FALSE;
ALTER TABLE request_logs ADD COLUMN pii_redacted_response BOOLEAN DEFAULT FALSE;

-- Backfill: existing pii_redacted=true rows came from GET responses (the
-- only path that ever set the flag), so backfill the response column from
-- the legacy column. Request column starts at false for legacy data.
UPDATE request_logs SET pii_redacted_response = pii_redacted WHERE pii_redacted = TRUE;

-- Note: keep pii_redacted (the legacy column) for one release for
-- read-side compatibility with rolled-back binaries; migration 008
-- (deferred) drops it once the dual-write window closes.
