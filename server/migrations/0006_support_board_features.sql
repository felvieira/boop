CREATE TABLE support_lists (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    position INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new','in_progress','blocked','done')),
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX support_lists_default_status ON support_lists(status) WHERE is_default=1;
CREATE UNIQUE INDEX support_lists_name_unique ON support_lists(lower(name));

INSERT INTO support_lists (id,name,position,status,is_default,created_at,updated_at) VALUES
    ('support-list-new','New',0,'new',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ('support-list-in-progress','In progress',1000,'in_progress',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ('support-list-blocked','Blocked',2000,'blocked',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ('support-list-done','Done',3000,'done',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'));

ALTER TABLE support_cards ADD COLUMN list_id TEXT REFERENCES support_lists(id);
ALTER TABLE support_cards ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE support_cards ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE support_cards ADD COLUMN due_at TEXT NOT NULL DEFAULT '';
ALTER TABLE support_cards ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','urgent'));
ALTER TABLE support_cards ADD COLUMN labels_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE support_cards ADD COLUMN archived_at TEXT NOT NULL DEFAULT '';

UPDATE support_cards SET list_id = CASE status
    WHEN 'in_progress' THEN 'support-list-in-progress'
    WHEN 'blocked' THEN 'support-list-blocked'
    WHEN 'done' THEN 'support-list-done'
    ELSE 'support-list-new'
END;

UPDATE support_cards
SET description = (SELECT t.digest FROM notification_triage t WHERE t.event_id=support_cards.source_message_id)
WHERE description='' AND source_account LIKE 'boop:%'
  AND EXISTS (SELECT 1 FROM notification_triage t WHERE t.event_id=support_cards.source_message_id AND t.digest<>'');

CREATE INDEX support_cards_list_position ON support_cards(list_id, archived_at, position, created_at DESC);

CREATE TABLE support_checklist_items (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    done INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0,1)),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX support_checklist_card_position ON support_checklist_items(card_id, position, created_at);

CREATE TABLE support_comments (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT 'Admin',
    created_at TEXT NOT NULL
);
CREATE INDEX support_comments_card_created ON support_comments(card_id, created_at, id);
