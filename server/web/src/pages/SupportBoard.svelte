<script lang="ts">
  import { api, type SupportCard, type SupportStatus } from '../lib/api'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import Empty from '../lib/ui/Empty.svelte'
  import Notice from '../lib/ui/Notice.svelte'

  const lanes: { status: SupportStatus; label: string }[] = [
    { status: 'new', label: 'New' },
    { status: 'in_progress', label: 'In progress' },
    { status: 'blocked', label: 'Blocked' },
    { status: 'done', label: 'Done' },
  ]
  let cards = $state<SupportCard[]>([])
  let loading = $state(true)
  let error = $state('')
  let busy = $state('')

  async function load() {
    try {
      error = ''
      cards = (await api.supportCards()).cards
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }
  async function move(card: SupportCard, status: SupportStatus) {
    if (card.status === status) return
    busy = card.id
    try {
      const updated = await api.setSupportCardStatus(card.id, status)
      cards = cards.map((item) => item.id === card.id ? updated : item)
    } catch (e: any) {
      error = e.message
    } finally {
      busy = ''
    }
  }
  function attachmentURL(card: SupportCard, attachmentId: string) {
    return `/api/v1/support/cards/${encodeURIComponent(card.id)}/attachments/${encodeURIComponent(attachmentId)}`
  }

  $effect(() => {
    load()
    const timer = setInterval(load, 30_000)
    return () => clearInterval(timer)
  })
</script>

<div class="stack">
  <div class="toolbar">
    <div>
      <h1>Support requests</h1>
      <p class="muted">Email reports with the original message and attached images.</p>
    </div>
    <span class="spacer"></span>
    <Button variant="secondary" size="sm" onclick={load} disabled={loading}>Refresh</Button>
  </div>
  {#if error}<Notice tone="bad">{error.includes('support_auth_required') ? 'The private support board is disabled. Configure BOOP_ADMIN_USER and BOOP_ADMIN_PASSWORD, then sign in.' : error}</Notice>{/if}

  {#if loading && cards.length === 0}
    <Card><div class="muted">Loading support requests…</div></Card>
  {:else if !error && cards.length === 0}
    <Card flush><Empty title="No support requests yet">When the email automation posts its first report, it will appear here.</Empty></Card>
  {:else}
    <div class="board" aria-label="Support request Kanban board">
      {#each lanes as lane (lane.status)}
        <section class="lane" aria-label={lane.label}>
          <div class="lane-head"><h2>{lane.label}</h2><span class="count">{cards.filter((card) => card.status === lane.status).length}</span></div>
          {#each cards.filter((card) => card.status === lane.status) as card (card.id)}
            <article class="ticket">
              <div class="meta"><span class="product">{card.product}</span><time datetime={card.reported_at}>{new Date(card.reported_at).toLocaleString()}</time></div>
              <h3>{card.title}</h3>
              <p class="sender">{card.sender || card.source_account} · {card.source_account}</p>
              <div class="message">{card.original_text}</div>
              {#if card.attachments.length}
                <div class="images" aria-label="Attached images">
                  {#each card.attachments as attachment (attachment.id)}
                    <a href={attachmentURL(card, attachment.id)} target="_blank" rel="noreferrer" aria-label={`Open ${attachment.filename}`}>
                      <img src={attachmentURL(card, attachment.id)} alt={attachment.filename} loading="lazy" />
                    </a>
                  {/each}
                </div>
              {/if}
              <div class="reference" title={`Conversation ${card.source_thread_id}; message ${card.source_message_id}`}>
                Email ref · {card.source_thread_id} / {card.source_message_id}
              </div>
              <label class="move">Move to
                <select value={card.status} disabled={busy === card.id} onchange={(e) => move(card, (e.currentTarget as HTMLSelectElement).value as SupportStatus)}>
                  {#each lanes as option}<option value={option.status}>{option.label}</option>{/each}
                </select>
              </label>
            </article>
          {/each}
          {#if !cards.some((card) => card.status === lane.status)}<div class="empty-lane">No cards</div>{/if}
        </section>
      {/each}
    </div>
  {/if}
</div>

<style>
  h1 { font: var(--up-type-wordmark); color: var(--up-ink); }
  h2 { font: var(--up-type-ui); color: var(--up-text-secondary); }
  h3 { font: var(--up-type-row-title); color: var(--up-ink); overflow-wrap: anywhere; }
  .toolbar { display: flex; align-items: center; gap: var(--up-space-3); }
  .toolbar p { margin-top: 5px; }
  .spacer { flex: 1; }
  .board { display: grid; grid-template-columns: repeat(4, minmax(220px, 1fr)); gap: var(--up-space-3); align-items: start; overflow-x: auto; padding-bottom: 8px; }
  .lane { min-width: 220px; padding: 10px; border-radius: var(--up-radius-card); background: #f6f6fa; min-height: 200px; }
  .lane-head { display: flex; justify-content: space-between; align-items: center; padding: 2px 2px 10px; }
  .count { color: var(--up-text-muted); font: var(--up-type-caption); }
  .ticket { background: var(--up-bg); border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-row); padding: 12px; margin-bottom: 10px; box-shadow: 0 2px 8px rgba(23,24,31,.04); display: flex; flex-direction: column; gap: 8px; }
  .meta { display: flex; justify-content: space-between; gap: 6px; align-items: center; color: var(--up-text-muted); font: var(--up-type-caption); }
  .meta time { text-align: right; }
  .product { color: var(--up-accent); font-weight: 700; }
  .sender { color: var(--up-text-secondary); font: var(--up-type-caption); overflow-wrap: anywhere; }
  .message { border-top: 1px solid var(--up-border-hairline); padding-top: 8px; font: var(--up-type-meta); white-space: pre-wrap; overflow-wrap: anywhere; max-height: 260px; overflow: auto; }
  .images { display: flex; gap: 6px; flex-wrap: wrap; }
  .images img { display: block; width: 68px; height: 68px; border-radius: var(--up-radius-cell); object-fit: cover; border: 1px solid var(--up-border-hairline); }
  .reference { color: var(--up-text-muted); font: var(--up-type-caption); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .move { display: flex; align-items: center; justify-content: space-between; gap: 8px; color: var(--up-text-secondary); font: var(--up-type-caption); }
  .move select { max-width: 130px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); padding: 5px 7px; background: var(--up-bg); color: var(--up-ink); font: var(--up-type-caption); }
  .empty-lane { padding: 18px 4px; text-align: center; color: var(--up-text-faint); font: var(--up-type-caption); }
  @media (max-width: 900px) { .board { grid-template-columns: repeat(4, minmax(250px, 1fr)); } }
</style>
