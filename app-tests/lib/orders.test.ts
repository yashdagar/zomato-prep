import { ApiClient } from '@app/api';
import { fetchOrder, isFinal, ordersPath, pollInterval, POLL_MS, statusLabel, timeline, type OrderDetail } from '@app/orders';
import { fakeFetch, json, memStore } from './fakes';

const detail: OrderDetail = {
  id: 9, restaurant_id: 5, status: 'PREPARING', total_paise: 39900, created_at: '2026-11-18T10:00:00Z',
  events: [
    { event_id: 'e1', from_status: '', to_status: 'PLACED', created_at: '2026-11-18T10:00:00Z' },
    { event_id: 'e2', from_status: 'PLACED', to_status: 'ACCEPTED', created_at: '2026-11-18T10:01:00Z' },
    { event_id: 'e3', from_status: 'ACCEPTED', to_status: 'PREPARING', created_at: '2026-11-18T10:03:00Z' },
  ],
};

describe('W4 B8 order history', () => {
  test('the first page of orders has no cursor and later pages carry it', () => {
    expect(ordersPath('')).toBe('/v1/orders?limit=20');
    expect(new URL('http://x' + ordersPath('c2')).searchParams.get('cursor')).toBe('c2');
  });

  test('the timeline lists each status change in the order it happened', () => {
    expect(timeline(detail).map(s => s.status)).toEqual(['PLACED', 'ACCEPTED', 'PREPARING']);
    expect(timeline(detail)[1]).toEqual({ status: 'ACCEPTED', label: 'Accepted by the restaurant', at: '2026-11-18T10:01:00Z' });
  });

  test('every status has a human label', () => {
    for (const s of ['PLACED', 'ACCEPTED', 'PREPARING', 'READY', 'PICKED_UP', 'DELIVERED', 'CANCELLED', 'REJECTED'] as const) {
      expect(statusLabel(s)).not.toBe(s);
    }
  });

  test('delivered, cancelled and rejected orders are final', () => {
    expect(isFinal('DELIVERED')).toBe(true);
    expect(isFinal('CANCELLED')).toBe(true);
    expect(isFinal('REJECTED')).toBe(true);
    expect(isFinal('PICKED_UP')).toBe(false);
  });

  test('an order still moving is polled every 10 s and a final one is not polled', () => {
    expect(POLL_MS).toBe(10_000);
    expect(pollInterval({ status: 'PREPARING' })).toBe(10_000);
    expect(pollInterval({ status: 'DELIVERED' })).toBe(false);
    expect(pollInterval(undefined)).toBe(10_000);
  });

  test('the detail is fetched by id with the bearer token', async () => {
    const calls = fakeFetch(() => json(200, detail));
    await expect(fetchOrder(new ApiClient('http://api', memStore({ access: 'A', refresh: 'R' })), 9)).resolves.toEqual(detail);
    expect(calls[0].url).toBe('http://api/v1/orders/9');
    expect(new Headers(calls[0].init.headers).get('Authorization')).toBe('Bearer A');
  });
});
