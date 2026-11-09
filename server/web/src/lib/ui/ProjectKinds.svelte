<script lang="ts">
  import { EVENT_KINDS, type EventKind, type ProjectStats } from '../api'
  import { KIND, netRevenue } from '../kinds'
  import ProjectIcon from './ProjectIcon.svelte'

  let { projects, active, onselect }: { projects: ProjectStats[]; active: string; onselect: (project: string, kind: EventKind | '') => void } = $props()

  // Only kinds that happened somewhere in the window earn a column.
  const columns = $derived(EVENT_KINDS.filter((k) => projects.some((p) => p.kinds[k] > 0)))
  const fmt = new Intl.NumberFormat()
  const showNet = $derived(projects.some((p) => netRevenue(p.revenue_cents) !== ''))
</script>

<div class="scroll">
  <table>
    <thead>
      <tr>
        <th class="name">Project</th>
        <th>Total</th>
        {#if showNet}<th title="Partial: sales + renewals − refunds from notifications with declared amounts">Declared net</th>{/if}
        {#each columns as k (k)}
          <th class:on={active === k} style="--kind: {KIND[k].color}" title={KIND[k].hint}><span class="dot" aria-hidden="true"></span>{KIND[k].label}</th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#each projects as p (p.project_id)}
        <tr>
          <th class="name" scope="row">
            <button type="button" class="cell proj" onclick={() => onselect(p.project_id, '')} title="Show only {p.project_name}">
              <ProjectIcon icon={p.project_icon} size={14} />
              <span>{p.project_name}</span>
            </button>
          </th>
          <td><button type="button" class="cell" onclick={() => onselect(p.project_id, '')}>{fmt.format(p.total)}</button></td>
          {#if showNet}<td class="net">{netRevenue(p.revenue_cents) || '·'}</td>{/if}
          {#each columns as k (k)}
            {@const n = p.kinds[k]}
            <td class:on={active === k}>
              {#if n > 0}
                <button type="button" class="cell" class:alert={KIND[k].good === 'down'} style="--kind: {KIND[k].color}" onclick={() => onselect(p.project_id, k)} title="{KIND[k].label} in {p.project_name}">{fmt.format(n)}</button>
              {:else}
                <span class="none">·</span>
              {/if}
            </td>
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .scroll { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font: var(--up-type-meta); font-variant-numeric: tabular-nums; }
  th, td { padding: 6px 8px; text-align: right; white-space: nowrap; border-top: 1px solid var(--up-border-hairline); }
  thead th { border-top: none; font: var(--up-type-caption); color: var(--up-text-secondary); }
  thead th.on { color: var(--up-ink); }
  .name { text-align: left; font-weight: inherit; }
  tbody th.name { max-width: 180px; }
  .dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: var(--kind); margin-right: 5px; vertical-align: 1px; }
  td.on { background: var(--up-bg-hover); }
  .cell { all: unset; cursor: pointer; color: var(--up-ink); border-radius: var(--up-radius-cell); padding: 1px 4px; }
  .cell:hover { background: var(--up-accent-tint); color: var(--up-accent-hover); }
  .cell:focus-visible { outline: 2px solid var(--up-accent-line); }
  .cell.alert { color: color-mix(in srgb, var(--kind) 80%, var(--up-ink)); font-weight: 600; }
  .proj { display: inline-flex; align-items: center; gap: 6px; max-width: 100%; color: var(--up-text-secondary); }
  .proj span { overflow: hidden; text-overflow: ellipsis; }
  .none { color: var(--up-text-faint); }
</style>
