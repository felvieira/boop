<script lang="ts">
  import { api, LEVELS, EVENT_KINDS, type Event, type EventKind, type EventStats, type Project } from '../lib/api'
  import { link, groupPath } from '../lib/router.svelte'
  import { relative, dayGroup, seenRange } from '../lib/format'
  import { LEVEL_LABEL } from '../lib/levels'
  import { KIND, PERIODS, isPeriod, periodSince, type Period } from '../lib/kinds'
  import Card from '../lib/ui/Card.svelte'
  import Select from '../lib/ui/Select.svelte'
  import Button from '../lib/ui/Button.svelte'
  import LevelBadge from '../lib/ui/LevelBadge.svelte'
  import Empty from '../lib/ui/Empty.svelte'
  import Notice from '../lib/ui/Notice.svelte'
  import ProjectIcon from '../lib/ui/ProjectIcon.svelte'
  import KindStats from '../lib/ui/KindStats.svelte'
  import ProjectKinds from '../lib/ui/ProjectKinds.svelte'
  import { rowIn, reorder, soft, panel } from '../lib/motion'
  import Skeleton from '../lib/ui/Skeleton.svelte'

  const params = typeof location !== 'undefined' ? new URLSearchParams(location.search) : new URLSearchParams()
  let events = $state<Event[]>([])
  let projects = $state<Project[]>([])
  let stats = $state<EventStats | null>(null)
  let cursor = $state<string | undefined>()
  let loading = $state(true)
  let loadingMore = $state(false)
  let error = $state('')
  let project = $state('')
  let level = $state('')
  let silenced = $state(params.get('silenced') ?? '')
  let kind = $state<EventKind | ''>((EVENT_KINDS as readonly string[]).includes(params.get('kind') ?? '') ? (params.get('kind') as EventKind) : '')
  // Rolling window shared by the big numbers and the list. Remembered per browser.
  let period = $state<Period>(readPeriod())
  // Start of the window the current list pages were loaded with.
  let since: string | undefined
  let now = $state(new Date())
  // Collapse repeats of the same fingerprint into one row. Remembered per browser.
  let grouped = $state(readGrouped())

  function readPeriod(): Period {
    try {
      const v = localStorage.getItem('boop.inbox.period')
      return isPeriod(v) ? v : '7d'
    } catch {
      return '7d'
    }
  }
  function setPeriod(v: string) {
    if (!isPeriod(v)) return
    period = v
    try {
      localStorage.setItem('boop.inbox.period', v)
    } catch {
      /* private mode etc. */
    }
  }

  function readGrouped(): boolean {
    try {
      return localStorage.getItem('boop.inbox.grouped') !== 'false'
    } catch {
      return true
    }
  }
  function setGrouped(v: boolean) {
    grouped = v
    try {
      localStorage.setItem('boop.inbox.grouped', String(v))
    } catch {
      /* private mode etc. */
    }
  }

  async function load(reset = true) {
    try {
      error = ''
      if (reset) {
        loading = true
        since = periodSince(period, new Date())
        void loadStats()
      } else loadingMore = true
      const page = await api.events({ project, level, silenced, kind, since, grouped, before: reset ? undefined : cursor, limit: 50 })
      events = reset ? page.events : [...events, ...page.events]
      cursor = page.next_cursor
      now = new Date()
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
      loadingMore = false
    }
  }

  // The big numbers ignore the kind filter so every count stays visible.
  async function loadStats() {
    try {
      stats = await api.eventStats({ project, level, silenced, since })
    } catch {
      stats = null
    }
  }

  function pick(p: string, k: EventKind | '') {
    project = p
    kind = k
  }

  // Live events refresh the counts; a burst costs one stats query.
  let statsTimer: ReturnType<typeof setTimeout> | undefined
  function scheduleStats() {
    clearTimeout(statsTimer)
    statsTimer = setTimeout(() => void loadStats(), 1000)
  }

  $effect(() => {
    project
    level
    silenced
    grouped
    kind
    period
    load(true)
  })
  $effect(() => {
    api.projects().then((r) => (projects = r.projects)).catch(() => {})
    const t = setInterval(() => load(true), 30_000)
    const tick = setInterval(() => (now = new Date()), 15_000)
    return () => {
      clearInterval(t)
      clearInterval(tick)
      clearTimeout(statsTimer)
    }
  })
  $effect(() => {
    const handleNew = (evt: globalThis.Event) => {
      const e = (evt as CustomEvent<Event>).detail
      if (!e || typeof e.id !== 'string') return
      if (!project || project === e.project_id) {
        if (!level || level === e.level) {
          if (silenced !== 'true') scheduleStats()
          if (!kind || kind === e.kind) {
            events = [e, ...events.filter((x) => x.id !== e.id)]
            now = new Date()
          }
        }
      }
    }
    window.addEventListener('boop:new_event', handleNew)
    return () => window.removeEventListener('boop:new_event', handleNew)
  })

  const groups = $derived.by(() => {
    const out: { label: string; items: Event[] }[] = []
    for (const e of events) {
      const label = dayGroup(e.created_at, now)
      const g = out[out.length - 1]
      if (g && g.label === label) g.items.push(e)
      else out.push({ label, items: [e] })
    }
    return out
  })
</script>

<div class="stack">
  <div class="toolbar">
    <Select value={period} onchange={(e) => setPeriod((e.currentTarget as HTMLSelectElement).value)} options={PERIODS.map((p) => ({ value: p.value, label: p.label }))} aria-label="Period" />
    <Select
      bind:value={project}
      options={[{ value: '', label: 'All projects' }, ...projects.map((p) => ({ value: p.id, label: p.name }))]}
      aria-label="Project"
    />
    <Select bind:value={kind} options={[{ value: '', label: 'All kinds' }, ...EVENT_KINDS.map((k) => ({ value: k, label: KIND[k].label }))]} aria-label="Kind" />
    <Select bind:value={level} options={[{ value: '', label: 'All levels' }, ...LEVELS.map((l) => ({ value: l, label: LEVEL_LABEL[l] }))]} aria-label="Level" />
    <Select bind:value={silenced} options={[{ value: '', label: 'Pushed and silenced' }, { value: 'true', label: 'Silenced only' }, { value: 'false', label: 'Pushed only' }]} aria-label="Silenced" />
    <label class="toggle caption">
      <input type="checkbox" checked={grouped} onchange={(e) => setGrouped((e.currentTarget as HTMLInputElement).checked)} />
      Group repeats
    </label>
    <span class="spacer"></span>
    <Button variant="secondary" size="sm" onclick={() => load(true)} disabled={loading}>Refresh</Button>
  </div>

  <Card compact>
    <KindStats {stats} active={kind} periodLabel={PERIODS.find((p) => p.value === period)?.label ?? ''} onselect={(k) => (kind = k)} />
  </Card>

  {#if !project && stats && stats.projects.length > 1}
    <Card compact title="By project">
      <ProjectKinds projects={stats.projects} active={kind} onselect={pick} />
    </Card>
  {/if}

  {#if error}
    <div transition:panel><Notice tone="bad">{error}</Notice></div>
  {/if}

  <Card flush>
    {#if loading && events.length === 0}
      <div class="group">&nbsp;</div>
      <Skeleton rows={6} />
    {:else if events.length === 0}
      <Empty title={kind ? `No ${KIND[kind].label.toLowerCase()} in this period` : silenced === 'true' ? 'No silenced events' : 'No events yet'}>
        {#if projects.length === 0}
          <a href="/projects" onclick={link}>Create a project</a> to get an API key, then send your first boop.
        {:else}
          Send one with curl or a client, or use the test button in <a href="/settings" onclick={link}>settings</a>.
        {/if}
      </Empty>
    {:else}
      {#each groups as g, gi (g.label)}
        <div class="group" in:soft>{g.label}</div>
        {#each g.items as e, i (e.id)}
          {@const repeats = e.group && e.group.count > 1 ? e.group : null}
          <a class="ev" href={repeats ? groupPath(e.project_id, e.fingerprint) : `/events/${e.id}`} onclick={link} in:rowIn={{ i: gi * 4 + i }} animate:reorder>
            <div class="proj">
              <ProjectIcon icon={e.project_icon} size={14} />
              <span>{e.project_name}</span>
            </div>
            <div class="main">
              <div class="title">
                <span class="t">{e.title}</span>
                {#if repeats}<span class="count" title="{repeats.count} occurrences of this fingerprint">×{repeats.count}</span>{/if}
                {#if e.kind && e.kind !== 'other' && e.kind !== 'error'}<span class="kind" style="--kind: {KIND[e.kind].color}">{KIND[e.kind].tag}</span>{/if}
              </div>
              {#if repeats}
                <div class="body seen">{seenRange(repeats.first_seen, repeats.last_seen, now)}</div>
              {:else if e.body}<div class="body">{e.body}</div>{/if}
            </div>
            <div class="lvl">
              <LevelBadge level={e.level} />
              {#if e.silenced}<span class="silenced caption">silenced</span>{/if}
            </div>
            <div class="time" title={e.created_at}>{relative(e.created_at, now)}</div>
          </a>
        {/each}
      {/each}
      {#if cursor}
        <div class="more">
          <Button variant="ghost" size="sm" onclick={() => load(false)} disabled={loadingMore}>{loadingMore ? 'Loading' : 'Load more'}</Button>
        </div>
      {/if}
    {/if}
  </Card>
</div>

<style>
  .toolbar { display: flex; gap: var(--up-space-3); align-items: center; flex-wrap: wrap; }
  .spacer { flex: 1; }
  .group {
    font: var(--up-type-caption);
    color: var(--up-text-faint);
    padding: 10px var(--up-space-4) 6px;
    text-transform: none;
  }
  .group + .ev { border-top: none; }
  .ev {
    display: grid;
    grid-template-columns: 120px minmax(0, 1fr) 40px 64px;
    gap: var(--up-space-4);
    align-items: center;
    padding: 12px var(--up-space-4);
    border-radius: var(--up-radius-control);
    color: var(--up-ink);
    transition: background 120ms ease-out;
  }
  .ev:hover { background: var(--up-bg-hover); color: var(--up-ink); }
  .proj { font: var(--up-type-meta); color: var(--up-text-secondary); display: flex; gap: 6px; align-items: center; min-width: 0; }
  .proj span:last-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .main { min-width: 0; }
  .title { font: var(--up-type-row-title); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: flex; align-items: baseline; gap: 8px; min-width: 0; }
  .title .t { overflow: hidden; text-overflow: ellipsis; }
  .count { font: var(--up-type-small); color: var(--up-accent-hover); background: var(--up-accent-tint); border-radius: var(--up-radius-pill); padding: 1px 8px; flex: none; }
  .kind { font: var(--up-type-caption); color: color-mix(in srgb, var(--kind) 75%, var(--up-ink)); background: color-mix(in srgb, var(--kind) 14%, var(--up-bg)); border-radius: var(--up-radius-pill); padding: 1px 8px; flex: none; }
  .seen { color: var(--up-text-secondary); }
  .toggle { display: inline-flex; align-items: center; gap: 6px; color: var(--up-text-secondary); cursor: pointer; user-select: none; }
  .toggle input { accent-color: var(--up-accent); margin: 0; }
  .body { font: var(--up-type-meta); color: var(--up-text-muted); margin-top: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .time { font: var(--up-type-caption); color: var(--up-text-muted); text-align: right; }
  .lvl { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; min-width: 0; }
  .silenced { color: var(--up-text-inactive); }
  .more { display: flex; justify-content: center; padding: 10px 0 4px; }
  @media (max-width: 600px) {
    .ev { grid-template-columns: 1fr auto; grid-template-rows: auto auto; }
    .proj { grid-column: 1; grid-row: 1; }
    .time { grid-column: 2; grid-row: 1; }
    .main { grid-column: 1 / span 2; grid-row: 2; }
    .lvl { display: none; }
  }
</style>
