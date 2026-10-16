CREATE TABLE notification_triage (
    event_id TEXT PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','processing','matched','ignored')),
    category TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    digest TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    lease_until TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX notification_triage_ready ON notification_triage(state, next_attempt_at, lease_until, created_at);

-- Backfill every event already stored by Boop. New events are added by the
-- triage worker's durable discovery sweep, so all integrations share one path.
INSERT INTO notification_triage (event_id, state, attempts, next_attempt_at, created_at, updated_at)
SELECT id, 'pending', 0, strftime('%Y-%m-%dT%H:%M:%S','now') || '.000000000Z',
       strftime('%Y-%m-%dT%H:%M:%S','now') || '.000000000Z',
       strftime('%Y-%m-%dT%H:%M:%S','now') || '.000000000Z'
FROM events;
