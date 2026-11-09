<script lang="ts">
  import type { EventKind, EventStats } from '../api'
  import { KIND, KIND_ROWS, trend, kindMoney, netRevenue } from '../kinds'

  let { stats, active, periodLabel, onselect }: { stats: EventStats | null; active: string; periodLabel: string; onselect: (kind: EventKind | '') => void } = $props()

  const fmt = new Intl.NumberFormat()
  const total = $derived(stats ? trend('total', stats.total, stats.previous) : null)
  const net = $derived(netRevenue(stats?.revenue_cents))
</script>

<section class="kinds" aria-label="Notifications by kind">
  <div class="head">
    <button type="button" class="all" class:on={!active} onclick={() => onselect('')}>
      <span class="n">{stats ? fmt.format(stats.total) : '–'}</span>
      <span class="what">notifications · {periodLabel.toLowerCase()}</span>
      {#if total}<span class="delta {total.tone}" title="Change against the previous period">{total.text}</span>{/if}
    </button>
    {#if net}<span class="net" title="Sales + renewals − refunds from notifications with declared amounts; not a complete accounting total">Declared net <strong>{net}</strong></span>{/if}
    {#if active}<button type="button" class="clear caption" onclick={() => onselect('')}>Clear filter</button>{/if}
  </div>
  {#if net}<p class="coverage">Partial: excludes notifications without a declared amount and currency, including older events.</p>{/if}
  {#each KIND_ROWS as row (row.label)}
    <div class="row-label caption">{row.label}</div>
    <div class="grid">
      {#each row.kinds as k (k)}
        {@const n = stats?.kinds[k] ?? 0}
        {@const t = stats ? trend(k, n, stats.previous) : null}
        <button
          type="button"
          class="big"
          class:on={active === k}
          class:zero={stats && n === 0}
          style="--kind: {KIND[k].color}"
          title={KIND[k].hint}
          aria-pressed={active === k}
          onclick={() => onselect(active === k ? '' : k)}
        >
          <span class="label"><span class="dot" aria-hidden="true"></span>{KIND[k].label}</span>
          <span class="value">{stats ? fmt.format(n) : '–'}</span>
          <span class="delta {t?.tone ?? 'neutral'}">{t?.text ?? ' '}</span>
          {#if stats}{@const money = kindMoney(stats.revenue_cents, k)}{#if money}<span class="money">{money}</span>{/if}{/if}
        </button>
      {/each}
    </div>
  {/each}
</section>

<style>
  .net { font: var(--up-type-meta); color: var(--up-text-secondary); margin-left: auto; }
  .coverage { font: var(--up-type-caption); color: var(--up-text-secondary); }
  .net strong { color: var(--up-ink); font-weight: 700; }
  .money { font: var(--up-type-caption); color: var(--up-text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-variant-numeric: tabular-nums; }
  .kinds { display: flex; flex-direction: column; gap: var(--up-space-2); }
  .row-label { color: var(--up-text-muted); margin-top: 4px; margin-bottom: -2px; }
  .head { display: flex; align-items: baseline; justify-content: space-between; gap: var(--up-space-3); margin-bottom: 2px; }
  .all { all: unset; cursor: pointer; display: inline-flex; align-items: baseline; gap: var(--up-space-2); border-radius: var(--up-radius-control); }
  .all .n { font: var(--up-type-metric); color: var(--up-ink); letter-spacing: -0.01em; }
  .all .what { font: var(--up-type-meta); color: var(--up-text-secondary); }
  .all.on .n { color: var(--up-accent-hover); }
  .all:focus-visible, .big:focus-visible { outline: 2px solid var(--up-accent-line); outline-offset: 2px; }
  .clear { all: unset; cursor: pointer; color: var(--up-accent); font: var(--up-type-caption); }
  .clear:hover { color: var(--up-accent-hover); }
  .grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: var(--up-space-2); }
  .big {
    all: unset;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    padding: 10px 12px;
    border: 1px solid var(--up-border-hairline);
    border-radius: var(--up-radius-row);
    background: var(--up-bg);
    box-shadow: inset 3px 0 0 var(--kind);
    transition: background-color 120ms ease-out, border-color 120ms ease-out;
  }
  .big:hover { background: var(--up-bg-hover); border-color: var(--up-border-control); }
  .big.on { border-color: var(--kind); background: color-mix(in srgb, var(--kind) 9%, var(--up-bg)); }
  .label { font: var(--up-type-caption); color: var(--up-text-secondary); display: flex; align-items: center; gap: 5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--kind); flex: none; }
  .value { font: 700 28px/1.1 var(--up-font); color: var(--up-ink); letter-spacing: -0.02em; font-variant-numeric: tabular-nums; }
  .zero .value { color: var(--up-text-inactive); }
  .delta { font: var(--up-type-caption); white-space: pre; font-variant-numeric: tabular-nums; }
  .good { color: var(--up-alt-mint); }
  .bad { color: #C9485E; }
  .neutral { color: var(--up-text-muted); }
  @media (max-width: 600px) {
    .grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
    .value { font-size: 24px; }
  }
</style>
