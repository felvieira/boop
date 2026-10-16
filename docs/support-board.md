# AI-curated support Kanban

Boop's **Support** tab reviews notifications from every connected project in four lanes: New, In progress, Blocked, and Done. The server sends notification text to OpenRouter's configured model, classifies actionable customer contacts, bug reports, corrections, suggestions, partner leads, and customer requests, then creates a card for each match. Routine billing, deployment, cron, and status notices stay in Inbox and are marked as filtered. Cards include the project, AI digest, sender/email when present in the original notification, timestamp, and original redacted notification. Card content is stored in the existing SQLite database.

Migration `0005_notification_triage.sql` queues every event already stored in Boop. A background worker discovers new events and processes both old and new notifications from the same durable queue. Failed OpenRouter calls are retried with backoff; card upserts use the event identity, so a retry cannot create a duplicate. The Support tab shows queue progress.

Configure the global OpenRouter key and model in Boop **Settings → AI support triage**. Boop validates a new key with OpenRouter's key endpoint without running a model completion, encrypts it on the server, and never returns it to the browser. The model picker shows JSON-capable text models and their published input/output prices per million tokens. Saved UI settings take precedence over the optional `OPENROUTER_API_KEY` and `BOOP_TRIAGE_MODEL` environment fallbacks; the default model is `google/gemini-3.8-flash`.

The encryption key is created next to the SQLite database with owner-only file permissions (by default, `/data/.boop-settings-key`). Keep that file with the database when backing up or restoring `/data`; if the key file is lost, Boop clears the unreadable saved provider key and asks for a new one in Settings. Email and phone strings, name fields, and common credential fields are omitted from the model prompt. Full stored notification text is retained in the private card so the owner can review the source.

Boop only processes events already ingested into its notification database. It does not log in to or read a mailbox; an email source must already forward its message into Boop as a notification event.

## Privacy and access

The board, card list, status changes, and image reads require Boop's existing administrator login. They stay closed when admin auth is disabled, even if older Boop routes are open behind a reverse proxy. Project API keys, device credentials, and the support-ingest token cannot read or change those routes. Image bytes are private/no-store and decoded as PNG, JPEG, or GIF before storage.

For a least-privilege automation path, Boop also accepts the optional BOOP_SUPPORT_INGEST_TOKEN only on POST /api/v1/support/cards/upsert. It is a write-only bearer token for idempotent card upserts; it cannot list cards, move statuses, read messages, or fetch images. Configure its value through the deployment secret manager and reference that same secret from an approved automation HTTP step. The application never generates or returns the token. Keep it unset until an owner explicitly approves creating that credential.

The existing administrator login is global and grants access to other Boop administration features. The optional write-only support-ingest token remains available for an external automation, but this built-in triage worker writes cards directly to SQLite and does not need that token or the admin password.

## Automation API

`POST /api/v1/support/cards/upsert` accepts `multipart/form-data` with:

- `metadata`: JSON with `source_account`, `source_thread_id`, `source_message_id`, `product`, `title`, `sender`, `reported_at` (RFC3339), and `original_text`.
- zero or more `attachments` parts containing image files (PNG/JPEG/GIF, 7 MB total maximum; request limit 8 MB).

The card is idempotent by the tuple `(source_account, source_thread_id, source_message_id)`. Replaying a message updates its subject/body/product/sender/time on the same card and preserves its status. If the replay includes images, those replace the existing attachment set; if no images are sent, the stored images are kept.

Example (the token must be referenced from an approved automation secret store):

```sh
curl --fail-with-body -H "Authorization: Bearer $BOOP_SUPPORT_INGEST_TOKEN" \
  -F 'metadata={"source_account":"support@example.test","source_thread_id":"thread-123","source_message_id":"message-456","product":"Boop","title":"Export fails","sender":"Customer <customer@example.test>","reported_at":"2026-10-11T12:00:00Z","original_text":"Synthetic example report"};type=application/json' \
  -F 'attachments=@screenshot.png;type=image/png' \
  https://boop.example.com/api/v1/support/cards/upsert
```

Other endpoints, also requiring admin auth:

- `GET /api/v1/support/cards` returns the board and attachment metadata.
- `PATCH /api/v1/support/cards/{id}/status` with `{"status":"in_progress"}` moves a card (`new`, `in_progress`, `blocked`, `done`).
- `GET /api/v1/support/cards/{id}/attachments/{attachment_id}` streams a private image.

The web board polls for updates every 30 seconds and can move cards between lanes. It does not send email or access Gmail.
