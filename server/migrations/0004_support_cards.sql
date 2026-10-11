CREATE TABLE support_cards (
    id TEXT PRIMARY KEY,
    source_account TEXT NOT NULL,
    source_thread_id TEXT NOT NULL,
    source_message_id TEXT NOT NULL,
    product TEXT NOT NULL,
    title TEXT NOT NULL,
    sender TEXT NOT NULL DEFAULT '',
    reported_at TEXT NOT NULL,
    original_text TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new','in_progress','blocked','done')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (source_account, source_thread_id, source_message_id)
);
CREATE INDEX support_cards_status_updated ON support_cards(status, updated_at DESC);
CREATE TABLE support_attachments (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL,
    content BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX support_attachments_card ON support_attachments(card_id);
