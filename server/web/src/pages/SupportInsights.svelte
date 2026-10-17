<script lang="ts">
  import { api, type SupportMetrics } from '../lib/api'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import Notice from '../lib/ui/Notice.svelte'

  let today = new Date().toLocaleDateString('en-CA')
  let monthAgoDate = new Date()
  monthAgoDate.setDate(monthAgoDate.getDate() - 29)
  let from = $state(monthAgoDate.toLocaleDateString('en-CA'))
  let to = $state(today)
  let data = $state<SupportMetrics | null>(null)
  let loading = $state(true)
  let error = $state('')

  async function load() {
    loading = true
    error = ''
    try {
      data = await api.supportMetrics(from, to)
    } catch (e: any) {
      error = e.message || 'Could not load support insights.'
    } finally {
      loading = false
    }
  }

  function applyRange(event: SubmitEvent) {
    event.preventDefault()
    void load()
  }

  function formatCount(value: number) {
    return new Intl.NumberFormat('en-US').format(value)
  }

  function formatCost(value: number | null) {
    if (value === null) return 'Not returned by provider'
    return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 6 }).format(value)
  }

  function formatDuration(value: number) {
    if (value < 1000) return `${value} ms`
    return `${(value / 1000).toFixed(1)} s`
  }

  function digestText(metrics: SupportMetrics) {
    const digest = metrics.daily_digest
    if (digest.new_cards === 0 && digest.urgent_active === 0 && digest.blocked_active === 0 && digest.overdue_active === 0) {
      return 'No new cards or urgent board items in the last 24 hours.'
    }
    const parts = [`${formatCount(digest.new_cards)} new ${digest.new_cards === 1 ? 'card' : 'cards'}`]
    if (digest.new_partnership_leads) parts.push(`${formatCount(digest.new_partnership_leads)} partnership ${digest.new_partnership_leads === 1 ? 'lead' : 'leads'}`)
    if (digest.urgent_active) parts.push(`${formatCount(digest.urgent_active)} urgent`)
    if (digest.blocked_active) parts.push(`${formatCount(digest.blocked_active)} blocked`)
    if (digest.overdue_active) parts.push(`${formatCount(digest.overdue_active)} overdue`)
    return `Last 24 hours: ${parts.join(' · ')}.`
  }
</script>

<section class="insights stack" aria-label="Support insights">
  <div class="insights-toolbar">
    <form class="range-form" onsubmit={applyRange}>
      <label><span>From</span><input bind:value={from} type="date" /></label>
      <label><span>To</span><input bind:value={to} type="date" /></label>
      <Button variant="secondary" size="sm" type="submit" disabled={loading}>Apply</Button>
    </form>
    <div class="export-actions">
      <a class="export-link" href={api.supportExportURL({ from, to }, 'csv')}>Export CSV</a>
      <a class="export-link" href={api.supportExportURL({ from, to }, 'json')}>Export JSON</a>
      <Button variant="secondary" size="sm" onclick={() => load()} disabled={loading}>Refresh</Button>
    </div>
  </div>

  {#if error}<Notice tone="bad"><span role="alert">{error}</span></Notice>{/if}
  {#if loading && !data}
    <Card><p class="muted">Loading support insights…</p></Card>
  {:else if data}
    <Card>
      <div class="digest-head"><div><h2>Daily digest</h2><p class="muted">A local summary of board changes. It does not call the model.</p></div><span>Last 24 hours</span></div>
      <p class="digest-copy">{digestText(data)}</p>
    </Card>

    <div class="metric-grid" aria-label="Selected period">
      <article class="metric"><span>Notifications processed</span><strong>{formatCount(data.notifications_processed)}</strong><small>{formatCount(data.matched)} cards · {formatCount(data.filtered)} filtered</small></article>
      <article class="metric"><span>Open failures</span><strong>{formatCount(data.open_failures)}</strong><small>{formatCount(data.retries)} retries and reprocesses</small></article>
      <article class="metric"><span>Cards created</span><strong>{formatCount(data.cards_created)}</strong><small>{formatCount(data.manual_cards_created)} created manually</small></article>
      <article class="metric"><span>Human corrections</span><strong>{formatCount(data.human_corrections)}</strong><small>Category or priority changed</small></article>
      <article class="metric"><span>AI usage</span><strong>{formatCount(data.total_tokens)}</strong><small>{formatCount(data.ai_runs)} runs · average {formatDuration(data.average_run_duration_ms)}</small></article>
      <article class="metric"><span>Provider cost</span><strong>{formatCost(data.cost_usd)}</strong><small>{formatCount(data.cost_measured_runs)} runs returned a cost value</small></article>
    </div>

    <div class="group-grid">
      <Card>
        <h2>Cards by project</h2>
        {#if data.by_product.length === 0}<p class="muted">No cards in this period.</p>{:else}<ul>{#each data.by_product as item (item.name)}<li><span>{item.name}</span><strong>{formatCount(item.count)}</strong></li>{/each}</ul>{/if}
      </Card>
      <Card>
        <h2>Cards by category</h2>
        {#if data.by_category.length === 0}<p class="muted">No cards in this period.</p>{:else}<ul>{#each data.by_category as item (item.name)}<li><span>{item.name.replace('_', ' ')}</span><strong>{formatCount(item.count)}</strong></li>{/each}</ul>{/if}
      </Card>
      <Card>
        <h2>Filtered notification reasons</h2>
        {#if data.filtered_by_reason.length === 0}<p class="muted">No filtered notifications in this period.</p>{:else}<ul>{#each data.filtered_by_reason as item (item.name)}<li><span>{item.name || 'No reason recorded'}</span><strong>{formatCount(item.count)}</strong></li>{/each}</ul>{/if}
      </Card>
    </div>
    <p class="footnote">Usage and cost are shown only when OpenRouter returns them with the response. Cost is provider-reported USD, not an estimate.</p>
  {/if}
</section>

<style>
  .insights { min-width: 0; gap: 14px; }
  .insights-toolbar, .range-form, .export-actions, .digest-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
  .range-form { justify-content: flex-start; }
  .range-form label { display: flex; align-items: center; gap: 6px; color: var(--up-text-muted); font: var(--up-type-caption); }
  .range-form input { min-height: 34px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); padding: 5px 8px; color: var(--up-ink); background: var(--up-bg); font: var(--up-type-meta); }
  .export-link { display: inline-flex; min-height: 34px; align-items: center; padding: 0 10px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); color: var(--up-text-secondary); font: var(--up-type-meta); text-decoration: none; }
  .export-link:hover { border-color: var(--up-accent); color: var(--up-accent-hover); }
  .digest-head h2, .group-grid h2 { color: var(--up-ink); font: var(--up-type-setting); }
  .digest-head p { margin-top: 4px; }
  .digest-head > span { padding: 4px 7px; border-radius: 5px; background: var(--up-accent-tint); color: var(--up-accent-hover); font: var(--up-type-caption); }
  .digest-copy { margin-top: 14px; color: var(--up-ink); font: var(--up-type-ui); }
  .metric-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 10px; }
  .metric { display: flex; min-height: 116px; flex-direction: column; gap: 7px; padding: 14px; border: 1px solid var(--up-border-hairline); border-radius: 9px; background: var(--up-bg); }
  .metric span, .metric small { color: var(--up-text-muted); font: var(--up-type-caption); }
  .metric strong { color: var(--up-ink); font: var(--up-type-wordmark); }
  .group-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 10px; align-items: start; }
  .group-grid h2 { margin-bottom: 12px; }
  .group-grid ul { display: flex; flex-direction: column; gap: 8px; margin: 0; padding: 0; list-style: none; }
  .group-grid li { display: flex; justify-content: space-between; gap: 8px; color: var(--up-text-secondary); font: var(--up-type-meta); }
  .group-grid li strong { color: var(--up-ink); }
  .footnote { color: var(--up-text-muted); font: var(--up-type-caption); }
  @media (max-width: 600px) { .insights-toolbar { align-items: flex-start; flex-direction: column; } .range-form { width: 100%; } }
</style>
