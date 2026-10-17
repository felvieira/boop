ALTER TABLE notification_triage ADD COLUMN reason TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_triage ADD COLUMN confidence REAL NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1);
ALTER TABLE notification_triage ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_triage ADD COLUMN processed_at TEXT NOT NULL DEFAULT '';

CREATE TABLE support_triage_audit (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action IN ('decision','retry','reprocess','failure')),
    state TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    digest TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
    model TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX support_triage_audit_event_created ON support_triage_audit(event_id, created_at DESC, id);
CREATE INDEX support_triage_audit_created ON support_triage_audit(created_at DESC, id);

INSERT INTO support_triage_audit (id,event_id,action,state,category,title,digest,reason,confidence,model,error,created_at)
SELECT 'audit_' || event_id, event_id,
       CASE WHEN state IN ('matched','ignored') THEN 'decision' ELSE 'failure' END,
       state, category, title, digest, reason, confidence, model, last_error, updated_at
FROM notification_triage
WHERE state IN ('matched','ignored') OR last_error <> '';

CREATE TABLE support_triage_runtime (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle','running','disabled','error')),
    last_started_at TEXT NOT NULL DEFAULT '',
    last_finished_at TEXT NOT NULL DEFAULT '',
    next_run_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
INSERT INTO support_triage_runtime (id,state,updated_at)
VALUES (1,'idle',strftime('%Y-%m-%dT%H:%M:%fZ','now'));
