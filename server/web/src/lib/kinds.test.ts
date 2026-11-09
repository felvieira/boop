import { describe, it, expect } from 'vitest'
import { trend, periodSince, netRevenue, kindMoney } from './kinds'
import { EVENT_KINDS, type KindCounts, type StatsWindow } from './api'

function window(counts: Partial<KindCounts>, total = 0): StatsWindow {
  const kinds = Object.fromEntries(EVENT_KINDS.map((k) => [k, counts[k] ?? 0])) as KindCounts
  return { total, kinds }
}

describe('trend', () => {
  it('reads more sales as good news and more cancellations as bad news', () => {
    const prev = window({ sale: 2, cancellation: 1 })
    expect(trend('sale', 5, prev)).toEqual({ text: '▲ 3', tone: 'good' })
    expect(trend('cancellation', 4, prev)).toEqual({ text: '▲ 3', tone: 'bad' })
    expect(trend('cancellation', 0, prev)).toEqual({ text: '▼ 1', tone: 'good' })
    expect(trend('sale', 1, prev)).toEqual({ text: '▼ 1', tone: 'bad' })
  })
  it('keeps totals and other neutral', () => {
    expect(trend('total', 9, window({}, 4))?.tone).toBe('neutral')
    expect(trend('other', 3, window({}))?.tone).toBe('neutral')
  })
  it('has nothing to compare without a previous window', () => {
    expect(trend('sale', 3, undefined)).toBeNull()
    expect(trend('sale', 0, window({}))?.text).toBe('—')
  })
})

describe('periodSince', () => {
  const now = new Date('2026-11-09T12:00:00Z')
  it('is a rolling window', () => {
    expect(periodSince('24h', now)).toBe('2026-11-08T12:00:00.000Z')
    expect(periodSince('7d', now)).toBe('2026-11-02T12:00:00.000Z')
    expect(periodSince('all', now)).toBeUndefined()
  })
})

describe('netRevenue', () => {
  it('subtracts refunds from sales and renewals per currency, ignoring failed charges', () => {
    const out = netRevenue({ BRL: { sale: 10000, renewal: 5000, refund: 2500, payment_failed: 9900 } })
    expect(out).toMatch(/125/)
    expect(out).not.toMatch(/224/)
  })
  it('is empty when nothing declared an amount', () => {
    expect(netRevenue(undefined)).toBe('')
    expect(kindMoney({ USD: { sale: 300 } }, 'refund')).toBe('')
  })
})
