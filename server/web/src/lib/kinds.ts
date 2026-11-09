import type { EventKind, Revenue, StatsWindow } from './api'

export interface KindMeta {
  label: string
  /** Singular, for the tag on one event. */
  tag: string
  hint: string
  color: string
  /** Which direction of change is good news for the owner. */
  good: 'up' | 'down' | 'none'
}

export const KIND: Record<EventKind, KindMeta> = {
  sale: { label: 'Sales', tag: 'Sale', hint: 'New subscriptions, orders and payments', color: 'var(--up-alt-mint)', good: 'up' },
  renewal: { label: 'Renewals', tag: 'Renewal', hint: 'Recurring charges that went through', color: 'var(--up-accent)', good: 'up' },
  trial: { label: 'Trials', tag: 'Trial', hint: 'Trials started', color: 'var(--up-alt-amber)', good: 'up' },
  signup: { label: 'Signups', tag: 'Signup', hint: 'New accounts', color: 'var(--up-operational-strong)', good: 'up' },
  lead: { label: 'Leads', tag: 'Lead', hint: 'Partnership and affiliate requests', color: 'var(--up-alt-blush)', good: 'up' },
  payment_failed: { label: 'Failed payments', tag: 'Payment failed', hint: 'Declined or failed charges', color: 'var(--up-degraded-strong)', good: 'down' },
  cancellation: { label: 'Cancellations', tag: 'Cancellation', hint: 'Subscriptions cancelled or not renewing', color: '#D9667A', good: 'down' },
  refund: { label: 'Refunds', tag: 'Refund', hint: 'Refunds and chargebacks', color: '#B98455', good: 'down' },
  error: { label: 'Errors', tag: 'Error', hint: 'Error and critical events', color: 'var(--up-outage-strong)', good: 'down' },
  other: { label: 'Other', tag: 'Other', hint: 'Everything else', color: 'var(--up-text-faint)', good: 'none' },
}

/** Big-number rows: money in, then money at risk and app health. */
export const KIND_ROWS: { label: string; kinds: EventKind[] }[] = [
  { label: 'Revenue and growth', kinds: ['sale', 'renewal', 'trial', 'signup', 'lead'] },
  { label: 'Churn and health', kinds: ['payment_failed', 'cancellation', 'refund', 'error', 'other'] },
]

export const PERIODS = [
  { value: '24h', label: 'Last 24 hours', ms: 24 * 3600_000 },
  { value: '7d', label: 'Last 7 days', ms: 7 * 24 * 3600_000 },
  { value: '30d', label: 'Last 30 days', ms: 30 * 24 * 3600_000 },
  { value: 'all', label: 'All time', ms: 0 },
] as const
export type Period = (typeof PERIODS)[number]['value']

export function isPeriod(v: string | null): v is Period {
  return PERIODS.some((p) => p.value === v)
}

/** RFC 3339 start of a rolling period, or undefined for all time. */
export function periodSince(period: Period, now: Date): string | undefined {
  const ms = PERIODS.find((p) => p.value === period)?.ms ?? 0
  return ms ? new Date(now.getTime() - ms).toISOString() : undefined
}

/** Change against the previous equally long window, toned by whether it is good news. */
export function trend(kind: EventKind | 'total', current: number, previous: StatsWindow | undefined): { text: string; tone: 'good' | 'bad' | 'neutral' } | null {
  if (!previous) return null
  const before = kind === 'total' ? previous.total : previous.kinds[kind]
  const diff = current - before
  if (diff === 0) return { text: before === 0 ? '—' : '= prev', tone: 'neutral' }
  const good = kind === 'total' ? 'none' : KIND[kind].good
  const better = good === 'none' ? null : (diff > 0) === (good === 'up')
  return { text: `${diff > 0 ? '▲' : '▼'} ${Math.abs(diff)}`, tone: better === null ? 'neutral' : better ? 'good' : 'bad' }
}

function formatMoney(cents: number, currency: string): string {
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency, maximumFractionDigits: cents % 100 ? 2 : 0 }).format(cents / 100)
  } catch {
    return `${currency} ${(cents / 100).toFixed(2)}`
  }
}

function joinMoney(entries: [string, number][]): string {
  return entries
    .filter(([, c]) => c !== 0)
    .sort((a, b) => Math.abs(b[1]) - Math.abs(a[1]))
    .map(([cur, c]) => formatMoney(c, cur))
    .join(' · ')
}

/** Declared amount of one kind, per currency ("R$ 1.240 · US$ 30"); '' when none. */
export function kindMoney(revenue: Revenue | undefined, kind: EventKind): string {
  return joinMoney(Object.entries(revenue ?? {}).map(([cur, k]) => [cur, k[kind] ?? 0]))
}

/** Sales plus renewals minus refunds, per currency; '' when nothing was declared. */
export function netRevenue(revenue: Revenue | undefined): string {
  return joinMoney(Object.entries(revenue ?? {}).map(([cur, k]) => [cur, (k.sale ?? 0) + (k.renewal ?? 0) - (k.refund ?? 0)]))
}
