-- Opt-in request keys do not change existing external_id semantics. Deleting an
-- event (including retention pruning) also removes its request key.
CREATE TABLE event_idempotency (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    request_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, request_key)
);
