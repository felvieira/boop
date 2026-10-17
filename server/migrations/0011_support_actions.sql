CREATE TABLE support_actions (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('github_issue','customer_reply','technical_plan')),
    state TEXT NOT NULL CHECK (state IN ('draft','approved','executing','completed','failed','needs_reconciliation','cancelled')),
    destination TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    external_url TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL UNIQUE,
    approved_by TEXT NOT NULL DEFAULT '',
    approved_at TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX support_actions_card_created ON support_actions(card_id,created_at DESC,id);
CREATE INDEX support_actions_state_updated ON support_actions(state,updated_at DESC);

CREATE TABLE support_action_audit (
    id TEXT PRIMARY KEY,
    action_id TEXT NOT NULL REFERENCES support_actions(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT 'Admin',
    summary TEXT NOT NULL DEFAULT '',
    payload_sha256 TEXT NOT NULL DEFAULT '',
    external_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX support_action_audit_history ON support_action_audit(action_id,created_at DESC,id);
