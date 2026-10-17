<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type SupportTriageAuditEntry, type SupportTriageEvent } from '../lib/api'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import Notice from '../lib/ui/Notice.svelte'

  let { onChanged }: { onChanged: () => void } = $props()
  const pageSize = 40
  let events = $state<SupportTriageEvent[]>([])
  let total = $state(0)
  let offset = $state(0)
  let stateFilter = $state('')
  let searchDraft = $state('')
  let search = $state('')
  let loading = $state(true)
  let error = $state('')
  let notice = $state('')
  let busyID = $state('')
  let confirmReprocessID = $state('')
  let expandedID = $state('')
  let history = $state<SupportTriageAuditEntry[]>([])
  let historyLoading = $state(false)

  const page = $derived(Math.floor(offset / pageSize) + 1)
  const pages = $derived(Math.max(1, Math.ceil(total / pageSize)))

  async function load() {
    loading = true
    error = ''
    try {
      const result = await api.supportTriageEvents({
        state: stateFilter || undefined,
        q: search || undefined,
        limit: pageSize,
        offset,
      })
      events = result.events
      total = result.total
      if (offset >= total && offset > 0) {
        offset = Math.max(0, (Math.ceil(total / pageSize) - 1) * pageSize)
      }
    } catch (e: any) {
      error = e.message || 'Could not load triage activity.'
    } finally {
      loading = false
    }
  }

  function applySearch(event: SubmitEvent) {
    event.preventDefault()
    search = searchDraft.trim()
    offset = 0
    void load()
  }

  function changeState(value: string) {
    stateFilter = value
    offset = 0
    void load()
  }

  async function toggleHistory(item: SupportTriageEvent) {
    if (expandedID === item.event_id) {
      expandedID = ''
      return
    }
    expandedID = item.event_id
    historyLoading = true
    try {
      const result = await api.supportTriageHistory(item.event_id)
      history = result.history
    } catch (e: any) {
      error = e.message || 'Could not load this notification history.'
      history = []
    } finally {
      historyLoading = false
    }
  }

  async function retry(item: SupportTriageEvent, reprocess = false) {
    busyID = item.event_id
    error = ''
    notice = ''
    try {
      await api.retrySupportTriageEvent(item.event_id, reprocess)
      confirmReprocessID = ''
      notice = reprocess ? 'Notification queued for explicit reclassification.' : 'Failed notification queued for retry.'
      expandedID = ''
      await load()
      onChanged()
    } catch (e: any) {
      error = e.message || 'Could not queue this notification.'
    } finally {
      busyID = ''
    }
  }

  function formatDate(value: string) {
    if (!value) return '—'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short' }).format(date)
  }

  function labelFor(item: SupportTriageEvent) {
    if (item.state === 'processing') return 'Processando'
    if (item.state === 'matched') return 'Cartão criado'
    if (item.state === 'ignored') return 'Filtrada'
    return item.last_error ? 'Falha · aguardando retry' : 'Aguardando'
  }

  function statusClass(item: SupportTriageEvent) {
    if (item.state === 'processing') return 'processing'
    if (item.state === 'matched') return 'matched'
    if (item.state === 'ignored') return 'ignored'
    return item.last_error ? 'failed' : 'pending'
  }

  function jsonText(value: string) {
    try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
  }

  onMount(() => {
    void load()
    const timer = setInterval(() => void load(), 30_000)
    return () => clearInterval(timer)
  })
</script>

<section class="activity stack" aria-label="AI triage activity">
  <div class="activity-toolbar">
    <form class="search" onsubmit={applySearch}>
      <span aria-hidden="true">⌕</span>
      <input bind:value={searchDraft} type="search" placeholder="Search notifications" aria-label="Search triage activity" />
      <Button variant="secondary" size="sm" type="submit" disabled={loading}>Search</Button>
    </form>
    <select value={stateFilter} onchange={(event) => changeState(event.currentTarget.value)} aria-label="Filter triage state">
      <option value="">All activity</option>
      <option value="pending">Waiting</option>
      <option value="processing">Processing</option>
      <option value="matched">Cards created</option>
      <option value="ignored">Filtered</option>
      <option value="failed">Failures</option>
    </select>
    <span class="result-count">{total} {total === 1 ? 'notification' : 'notifications'}</span>
  </div>

  {#if notice}<p class="notice-line" role="status">{notice}</p>{/if}
  {#if error}<Notice tone="bad"><span role="alert">{error}</span></Notice>{/if}
  {#if loading && events.length === 0}
    <Card><p class="muted">Loading triage activity…</p></Card>
  {:else if events.length === 0}
    <Card><p class="muted">No triage activity matches these filters.</p></Card>
  {:else}
    <div class="event-list">
      {#each events as item (item.event_id)}
        <article class="event-card" class:expanded={expandedID === item.event_id}>
          <header class="event-head">
            <div class="event-heading">
              <span class={`state ${statusClass(item)}`}>{labelFor(item)}</span>
              <strong>{item.title || item.event_title}</strong>
              <span class="event-meta">{item.project} · {item.source || 'Boop'} · {formatDate(item.occurred_at)}</span>
            </div>
            {#if item.state === 'pending' && item.last_error}
              <Button size="sm" onclick={() => retry(item)} disabled={busyID === item.event_id}>{busyID === item.event_id ? 'Queuing…' : 'Retry'}</Button>
            {:else if item.state === 'matched' || item.state === 'ignored'}
              <Button variant="secondary" size="sm" onclick={() => { confirmReprocessID = confirmReprocessID === item.event_id ? '' : item.event_id }} disabled={busyID === item.event_id}>Reclassify</Button>
            {/if}
          </header>

          <div class="decision-grid">
            <div><span class="key">Decision</span><span>{item.category || (item.state === 'ignored' ? 'Routine / no action' : '—')} · {item.priority}</span></div>
            <div><span class="key">Confidence</span><span>{item.confidence ? `${Math.round(item.confidence * 100)}%` : '—'}</span></div>
            <div><span class="key">Attempts</span><span>{item.attempts}</span></div>
            <div><span class="key">Model / prompt</span><span>{item.model || '—'}{item.prompt_version ? ` · ${item.prompt_version}` : ''}</span></div>
          </div>

          {#if item.digest}<p class="digest">{item.digest}</p>{/if}
          {#if item.reason}<p class="reason"><strong>Why:</strong> {item.reason}</p>{/if}
          {#if item.last_error}<Notice tone="bad"><strong>Last error:</strong> {item.last_error}{#if item.next_attempt_at}<span> · Next retry {formatDate(item.next_attempt_at)}</span>{/if}</Notice>{/if}
          {#if item.state === 'processing' && item.lease_until}<p class="muted small">Lease until {formatDate(item.lease_until)}</p>{/if}

          {#if confirmReprocessID === item.event_id}
            <div class="confirm-reprocess" role="group" aria-label="Confirm reclassification">
              <p>This sends the notification to the selected model again. Existing card status and list are preserved.</p>
              <Button size="sm" onclick={() => retry(item, true)} disabled={busyID === item.event_id}>{busyID === item.event_id ? 'Queuing…' : 'Confirm reclassification'}</Button>
              <Button variant="secondary" size="sm" onclick={() => (confirmReprocessID = '')}>Cancel</Button>
            </div>
          {/if}

          <div class="event-actions">
            {#if item.card_id}<span class="muted small">Linked card: {item.card_id} · {item.card_status || 'new'}</span>{/if}
            <Button variant="secondary" size="sm" onclick={() => toggleHistory(item)} aria-expanded={expandedID === item.event_id}>{expandedID === item.event_id ? 'Hide details' : 'Notification and history'}</Button>
          </div>

          {#if expandedID === item.event_id}
            <div class="event-details">
              <section><h3>Original notification</h3><strong>{item.event_title}</strong><p>{item.event_body || 'No body'}</p><pre>{jsonText(item.event_data)}</pre></section>
              <section><h3>Decision history</h3>
                {#if historyLoading}<p class="muted">Loading history…</p>
                {:else if history.length === 0}<p class="muted">No recorded decisions yet.</p>
                {:else}<ol class="history">{#each history as entry (entry.id)}<li><div><strong>{entry.action}</strong><span class="muted">{entry.state} · {entry.category} · {entry.priority} · {formatDate(entry.created_at)} · {entry.model || '—'}{entry.prompt_version ? ` · prompt ${entry.prompt_version}` : ''}</span></div>{#if entry.reason}<p>{entry.reason}</p>{/if}{#if entry.error}<p class="history-error">{entry.error}</p>{/if}</li>{/each}</ol>{/if}
              </section>
            </div>
          {/if}
        </article>
      {/each}
    </div>
    <nav class="pagination" aria-label="Triage activity pages">
      <Button variant="secondary" size="sm" onclick={() => { offset = Math.max(0, offset - pageSize); void load() }} disabled={offset === 0 || loading}>Previous</Button>
      <span class="muted small">Page {page} of {pages}</span>
      <Button variant="secondary" size="sm" onclick={() => { offset += pageSize; void load() }} disabled={offset + pageSize >= total || loading}>Next</Button>
    </nav>
  {/if}
</section>

<style>
  .activity { gap: 12px; min-width: 0; }
  .activity-toolbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .search { display: flex; flex: 1 1 300px; align-items: center; gap: 7px; min-width: 220px; height: 38px; padding: 0 7px 0 10px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); color: var(--up-text-muted); background: var(--up-bg); }
  .search input { flex: 1; min-width: 0; border: 0; outline: 0; color: var(--up-ink); font: var(--up-type-meta); background: transparent; }
  .activity-toolbar select { min-height: 38px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); padding: 6px 10px; color: var(--up-ink); background: var(--up-bg); font: var(--up-type-meta); }
  .result-count { margin-left: auto; color: var(--up-text-muted); font: var(--up-type-caption); }
  .event-list { display: flex; flex-direction: column; gap: 10px; }
  .event-card { display: flex; flex-direction: column; gap: 10px; padding: 14px; border: 1px solid var(--up-border-hairline); border-radius: 9px; background: var(--up-bg); }
  .event-card.expanded { border-color: var(--up-border-control); }
  .event-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
  .event-heading { display: flex; min-width: 0; flex-direction: column; gap: 5px; }
  .event-heading strong { color: var(--up-ink); font: var(--up-type-ui); overflow-wrap: anywhere; }
  .event-meta, .small { color: var(--up-text-muted); font: var(--up-type-caption); }
  .state { width: fit-content; padding: 3px 7px; border-radius: 5px; font: var(--up-type-caption); font-weight: 700; }
  .state.pending { color: #775716; background: #fbf2cf; }
  .state.failed { color: #973c47; background: #f9e5e7; }
  .state.processing { color: #395b91; background: #e7eefb; }
  .state.matched { color: #367454; background: #e3f3e8; }
  .state.ignored { color: #596071; background: #e9ebef; }
  .decision-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
  .decision-grid > div { display: flex; min-width: 0; flex-direction: column; gap: 3px; color: var(--up-text-secondary); font: var(--up-type-caption); overflow-wrap: anywhere; }
  .key { color: var(--up-text-muted); }
  .digest, .reason { color: var(--up-text-secondary); font: var(--up-type-meta); white-space: pre-wrap; overflow-wrap: anywhere; }
  .reason { color: var(--up-text-muted); font-size: .82rem; }
  .event-actions { display: flex; align-items: center; justify-content: space-between; gap: 9px; flex-wrap: wrap; padding-top: 5px; border-top: 1px solid var(--up-border-hairline); }
  .confirm-reprocess { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 10px; border: 1px solid #ead9a3; border-radius: 7px; background: #fffaf0; }
  .confirm-reprocess p { flex: 1 1 100%; color: var(--up-text-secondary); font: var(--up-type-caption); }
  .event-details { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; padding-top: 10px; border-top: 1px solid var(--up-border-hairline); }
  .event-details section { display: flex; min-width: 0; flex-direction: column; gap: 8px; }
  .event-details h3 { color: var(--up-text-secondary); font: var(--up-type-ui); }
  .event-details p, .event-details strong { color: var(--up-ink); font: var(--up-type-caption); overflow-wrap: anywhere; white-space: pre-wrap; }
  .event-details pre { max-height: 220px; overflow: auto; margin: 0; padding: 9px; border-radius: 6px; background: #f4f5f9; color: var(--up-text-secondary); font: var(--up-type-caption); white-space: pre-wrap; overflow-wrap: anywhere; }
  .history { display: flex; flex-direction: column; gap: 8px; margin: 0; padding: 0; list-style: none; }
  .history li { display: flex; flex-direction: column; gap: 5px; padding: 8px; border: 1px solid var(--up-border-hairline); border-radius: 6px; }
  .history li div { display: flex; justify-content: space-between; gap: 8px; color: var(--up-ink); font: var(--up-type-caption); }
  .history-error { color: #973c47 !important; }
  .pagination { display: flex; align-items: center; justify-content: center; gap: 12px; padding: 4px; }
  .notice-line { color: #287a59; font: var(--up-type-caption); }
  @media (max-width: 700px) {
    .event-head { align-items: stretch; flex-direction: column; }
    .event-head :global(button) { align-self: flex-start; }
    .decision-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .event-details { grid-template-columns: 1fr; }
  }
</style>
