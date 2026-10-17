# AI-curated support board

Boop's **Support** tab is a private Trello-style board for actionable customer contacts, bug reports, corrections, suggestions, partner leads, and customer requests from every connected project. It starts with New, In progress, Blocked, and Done lists; admins can add, rename, reorder, and remove empty custom lists. Cards can be created manually or by AI triage, moved with drag and drop or keyboard controls, edited, searched, filtered by project, and archived or restored. Card details support descriptions, labels, priorities, due dates, checklists, comments, and image attachments. All board data is stored in SQLite.

The triage worker discovers notifications at startup and once per hour after that. It processes queued notifications in batches only when they are pending or due for retry. The durable `notification_triage` row records every matched or ignored event, so completed notifications are not sent to the model again. On the first run, pending notifications already in the queue are processed too. Routine billing, deployment, cron, and status notices remain in Inbox and are marked as filtered. AI cards retain the source project, sender/email when present, timestamp, and original notification for review.

Migration `0005_notification_triage.sql` queues every event already stored in Boop. A background worker discovers new events and processes both old and new notifications from the same durable queue. Failed OpenRouter calls are retried with backoff; card upserts use the event identity, so a retry cannot create a duplicate. The Support tab shows queue progress.

Configure the global OpenRouter key and model in Boop **Settings → AI support triage**. Boop validates a new key with OpenRouter's key endpoint without running a model completion, encrypts it on the server, and never returns it to the browser. The model picker shows JSON-capable text models and their published input/output prices per million tokens. Saved UI settings take precedence over the optional `OPENROUTER_API_KEY` and `BOOP_TRIAGE_MODEL` environment fallbacks; the default model is `google/gemini-3.8-flash`.

The encryption key is created next to the SQLite database with owner-only file permissions (by default, `/data/.boop-settings-key`). Keep that file with the database when backing up or restoring `/data`; if the key file is lost, Boop clears the unreadable saved provider key and asks for a new one in Settings. Email and phone strings, name fields, and common credential fields are omitted from the model prompt. Full stored notification text is retained in the private card so the owner can review the source.

Boop only processes events already ingested into its notification database. It does not log in to or read a mailbox; an email source must already forward its message into Boop as a notification event.

## Privacy and access

The board, card and list changes, comments, checklists, archive, and image reads require Boop's existing administrator login. They stay closed when admin auth is disabled, even if older Boop routes are open behind a reverse proxy. Project API keys, device credentials, and the support-ingest token cannot read or change those routes. Image bytes are private/no-store and decoded as PNG, JPEG, or GIF before storage.

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

- `GET /api/v1/support/board` returns lists, cards, and attachment metadata. Add `?archived=true` to include archived cards.
- `POST /api/v1/support/lists`, `PATCH /api/v1/support/lists/{id}`, and `DELETE /api/v1/support/lists/{id}` manage custom lists. Lists that still contain cards cannot be deleted; the four default lists are permanent.
- `POST /api/v1/support/cards` and `PATCH /api/v1/support/cards/{id}` create and edit board cards.
- `POST /api/v1/support/cards/{id}/move` with `{"list_id":"...","position":0}` moves a card to a list and position. The legacy `PATCH /api/v1/support/cards/{id}/status` endpoint still moves cards to the four matching default lists.
- `POST /api/v1/support/cards/{id}/archive` and `/restore` archive and restore cards.
- `POST /api/v1/support/cards/{id}/comments` and `POST /api/v1/support/cards/{id}/checklist` add card activity and checklist items.
- `GET /api/v1/support/cards/{id}/attachments/{attachment_id}` streams a private image.

The web board polls for card changes every 30 seconds. It does not send email or access Gmail.

## Prepared actions and approvals

Card details can hold a GitHub issue draft, a customer reply draft, or a technical plan. Configure an encrypted GitHub personal access token in **Settings → GitHub issue approvals** and map each Boop product to an `owner/repository`. A card can override the mapped repository, and the action screen shows the destination and full issue content before the administrator confirms creation. The token needs Issues read and write access only to repositories used by the board.

GitHub issues include a Boop action marker so retries can find an existing issue. If a request times out after GitHub may have accepted it, the action enters reconciliation; Boop checks recent issues before it offers a retry. The action history records the administrator, payload hash, result, error, and GitHub issue id. A retry after an uncertain result requires the administrator to confirm that no issue exists.

Reply drafts can be approved and copied or opened in the user's email client. Boop never sends the reply. Approved technical plans become checklist items on that card. These actions and their history require the private Support admin authorization. Migration `0011_support_actions.sql` creates the action and audit tables.

New endpoints:

- `GET` and `POST /api/v1/support/cards/{id}/actions` list or prepare card actions.
- `PATCH` and `DELETE /api/v1/support/actions/{id}` edit or cancel an unapproved draft.
- `GET /api/v1/support/actions/{id}/history` reads its audit history.
- `POST /api/v1/support/actions/{id}/approve-reply`, `/execute`, `/reconcile`, and `/authorize-retry` approve a reply, execute a confirmed issue/plan, reconcile GitHub, or explicitly authorize a retry.
