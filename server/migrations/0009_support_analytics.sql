CREATE TABLE support_triage_runs (
    id TEXT PRIMARY KEY,
    model TEXT NOT NULL,
    purpose TEXT NOT NULL DEFAULT 'triage' CHECK (purpose IN ('triage','preview')),
    outcome TEXT NOT NULL CHECK (outcome IN ('success','partial','failed')),
    event_count INTEGER NOT NULL CHECK (event_count >= 0),
    matched_count INTEGER NOT NULL DEFAULT 0 CHECK (matched_count >= 0),
    ignored_count INTEGER NOT NULL DEFAULT 0 CHECK (ignored_count >= 0),
    failed_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
    completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    total_tokens INTEGER NOT NULL DEFAULT 0 CHECK (total_tokens >= 0),
    cost_usd REAL CHECK (cost_usd IS NULL OR cost_usd >= 0),
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL
);
CREATE INDEX support_triage_runs_finished ON support_triage_runs(finished_at DESC, outcome);
CREATE INDEX support_triage_runs_model ON support_triage_runs(model, finished_at DESC);
CREATE INDEX support_triage_runs_purpose ON support_triage_runs(purpose, finished_at DESC);

CREATE TABLE support_digest_deliveries (
    local_date TEXT NOT NULL,
    device_id TEXT NOT NULL,
    sent_at TEXT NOT NULL,
    PRIMARY KEY (local_date, device_id)
);
CREATE INDEX support_digest_deliveries_sent ON support_digest_deliveries(sent_at DESC);

CREATE TABLE support_card_links (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    related_card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    relation TEXT NOT NULL CHECK (relation IN ('related','duplicate')),
    created_at TEXT NOT NULL,
    CHECK (card_id <> related_card_id),
    UNIQUE (card_id,related_card_id,relation)
);
CREATE INDEX support_card_links_card ON support_card_links(card_id,created_at DESC);
CREATE INDEX support_card_links_related ON support_card_links(related_card_id,created_at DESC);

CREATE TABLE support_card_link_audit (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL,
    related_card_id TEXT NOT NULL,
    relation TEXT NOT NULL CHECK (relation IN ('related','duplicate')),
    action TEXT NOT NULL CHECK (action IN ('linked','unlinked')),
    actor TEXT NOT NULL DEFAULT 'Admin',
    created_at TEXT NOT NULL
);
CREATE INDEX support_card_link_audit_card ON support_card_link_audit(card_id,created_at DESC);
