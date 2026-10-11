# Private support Kanban

Boop's **Support** tab tracks customer email reports in four lanes: New, In progress, Blocked, and Done. Each card keeps the product, subject, sender, reported time, original plain-text report, Gmail account/conversation/message identifiers, and any attached screenshots. Card content and image bytes are stored in the existing SQLite database.

## Privacy and access

The board, card list, status changes, and image reads require Boop's existing administrator login. They stay closed when admin auth is disabled, even if older Boop routes are open behind a reverse proxy. Project API keys, device credentials, and the support-ingest token cannot read or change those routes. Image bytes are private/no-store and decoded as PNG, JPEG, or GIF before storage.

For a least-privilege automation path, Boop also accepts the optional BOOP_SUPPORT_INGEST_TOKEN only on POST /api/v1/support/cards/upsert. It is a write-only bearer token for idempotent card upserts; it cannot list cards, move statuses, read messages, or fetch images. Configure its value through the deployment secret manager and reference that same secret from an approved automation HTTP step. The application never generates or returns the token. Keep it unset until an owner explicitly approves creating that credential.

The existing administrator login is global and grants access to other Boop administration features. Do not add that password to the Gmail automation unless the account owner explicitly approves that broader access. No token is generated or configured by this implementation, and it does not connect to Gmail.

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
