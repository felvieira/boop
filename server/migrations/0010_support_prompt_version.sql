ALTER TABLE notification_triage ADD COLUMN prompt_version TEXT NOT NULL DEFAULT '';
ALTER TABLE support_triage_audit ADD COLUMN prompt_version TEXT NOT NULL DEFAULT '';
