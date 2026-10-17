ALTER TABLE notification_triage ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal'
    CHECK (priority IN ('low','normal','high','urgent'));
ALTER TABLE support_triage_audit ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal'
    CHECK (priority IN ('low','normal','high','urgent'));

ALTER TABLE support_cards ADD COLUMN category TEXT NOT NULL DEFAULT 'other'
    CHECK (category IN ('bug','correction','suggestion','partnership','customer_request','internal_bug','other'));
ALTER TABLE support_cards ADD COLUMN ai_category TEXT NOT NULL DEFAULT '';
ALTER TABLE support_cards ADD COLUMN ai_priority TEXT NOT NULL DEFAULT ''
    CHECK (ai_priority IN ('','low','normal','high','urgent'));
ALTER TABLE support_cards ADD COLUMN ai_confidence REAL NOT NULL DEFAULT 0
    CHECK (ai_confidence >= 0 AND ai_confidence <= 1);
ALTER TABLE support_cards ADD COLUMN ai_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE support_cards ADD COLUMN review_state TEXT NOT NULL DEFAULT 'reviewed'
    CHECK (review_state IN ('needs_review','reviewed'));
ALTER TABLE support_cards ADD COLUMN reviewed_at TEXT NOT NULL DEFAULT '';
ALTER TABLE support_cards ADD COLUMN category_override INTEGER NOT NULL DEFAULT 0
    CHECK (category_override IN (0,1));
ALTER TABLE support_cards ADD COLUMN priority_override INTEGER NOT NULL DEFAULT 0
    CHECK (priority_override IN (0,1));

CREATE INDEX support_cards_category_review ON support_cards(category,review_state,archived_at,updated_at DESC);
CREATE INDEX support_cards_review_updated ON support_cards(review_state,archived_at,updated_at DESC);

UPDATE support_cards
SET category=(SELECT t.category FROM notification_triage t WHERE t.event_id=support_cards.source_message_id),
    ai_category=(SELECT t.category FROM notification_triage t WHERE t.event_id=support_cards.source_message_id),
    ai_priority=(SELECT t.priority FROM notification_triage t WHERE t.event_id=support_cards.source_message_id),
    review_state='needs_review'
WHERE source_account LIKE 'boop:%'
  AND EXISTS (SELECT 1 FROM notification_triage t WHERE t.event_id=support_cards.source_message_id AND t.state='matched');

UPDATE support_cards
SET priority_override=1
WHERE source_account LIKE 'boop:%'
  AND priority<>(SELECT t.priority FROM notification_triage t WHERE t.event_id=support_cards.source_message_id);

CREATE TABLE support_classification_feedback (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    field TEXT NOT NULL CHECK (field IN ('category','priority')),
    predicted_value TEXT NOT NULL,
    corrected_value TEXT NOT NULL,
    product TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(card_id,field,predicted_value,corrected_value)
);
CREATE INDEX support_feedback_pattern ON support_classification_feedback(product,field,predicted_value,corrected_value,created_at);

CREATE TABLE support_classification_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    product TEXT NOT NULL DEFAULT '',
    field TEXT NOT NULL CHECK (field IN ('category','priority')),
    when_value TEXT NOT NULL,
    set_value TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    dry_run INTEGER NOT NULL DEFAULT 1 CHECK (dry_run IN (0,1)),
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','learned')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX support_rules_identity ON support_classification_rules(lower(product),field,lower(when_value));
CREATE INDEX support_rules_enabled ON support_classification_rules(enabled,dry_run,field,product);

CREATE TABLE support_rule_firings (
    id TEXT PRIMARY KEY,
    rule_id TEXT REFERENCES support_classification_rules(id) ON DELETE SET NULL,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    card_id TEXT REFERENCES support_cards(id) ON DELETE SET NULL,
    product TEXT NOT NULL DEFAULT '',
    field TEXT NOT NULL CHECK (field IN ('category','priority')),
    outcome TEXT NOT NULL CHECK (outcome IN ('simulated','applied')),
    before_value TEXT NOT NULL,
    after_value TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX support_rule_firings_event ON support_rule_firings(event_id,created_at DESC);
CREATE INDEX support_rule_firings_created ON support_rule_firings(created_at DESC,outcome);

CREATE TABLE support_card_audit (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES support_cards(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action IN ('created','edited','classified','moved','archived','restored','reviewed')),
    field TEXT NOT NULL DEFAULT '',
    old_value TEXT NOT NULL DEFAULT '',
    new_value TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL DEFAULT 'Admin',
    created_at TEXT NOT NULL
);
CREATE INDEX support_card_audit_history ON support_card_audit(card_id,created_at DESC,id);

INSERT INTO support_card_audit(id,card_id,action,field,new_value,actor,created_at)
SELECT 'card_created_' || id,id,'created','category',category,'Migration',created_at FROM support_cards;
