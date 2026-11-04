import type { OrderDetail } from '@app/orders';
import { applyEvent, parseSocketMessage } from '@app/tracking';

const order: OrderDetail = {
  id: 9, restaurant_id: 5, status: 'ACCEPTED', total_paise: 39900, created_at: '2026-11-18T10:00:00Z',
  events: [
    { event_id: 'e1', from_status: '', to_status: 'PLACED', created_at: '2026-11-18T10:00:00Z' },
    { event_id: 'e2', from_status: 'PLACED', to_status: 'ACCEPTED', created_at: '2026-11-18T10:01:00Z' },
  ],
};
const now = () => Date.parse('2026-11-18T10:05:00Z');

describe('W5 B6 live order tracking', () => {
  test('an event for this order moves the status and extends the timeline', () => {
    const next = applyEvent(order, { order_id: 9, event_id: 'e3', from: 'ACCEPTED', to: 'PREPARING' }, now);
    expect(next.status).toBe('PREPARING');
    expect(next.events).toHaveLength(3);
    expect(next.events[2]).toEqual({ event_id: 'e3', from_status: 'ACCEPTED', to_status: 'PREPARING', created_at: '2026-11-18T10:05:00.000Z' });
  });

  test('the original order is not mutated', () => {
    applyEvent(order, { order_id: 9, event_id: 'e3', from: 'ACCEPTED', to: 'PREPARING' }, now);
    expect(order.status).toBe('ACCEPTED');
    expect(order.events).toHaveLength(2);
  });

  test('an event that was already applied is ignored', () => {
    const next = applyEvent(order, { order_id: 9, event_id: 'e2', from: 'PLACED', to: 'ACCEPTED' }, now);
    expect(next).toBe(order);
  });

  test('an event for another order is ignored', () => {
    const next = applyEvent(order, { order_id: 10, event_id: 'e9', from: 'PLACED', to: 'ACCEPTED' }, now);
    expect(next).toBe(order);
  });

  test('a status event parses with its order, id and transition', () => {
    expect(parseSocketMessage('{"order_id":9,"event_id":"e3","from":"ACCEPTED","to":"PREPARING"}')).toEqual({
      kind: 'event',
      event: { order_id: 9, event_id: 'e3', from: 'ACCEPTED', to: 'PREPARING' },
    });
  });

  test('a location message parses to a position', () => {
    expect(parseSocketMessage('{"lat":28.4601,"lng":77.0302}')).toEqual({ kind: 'location', lat: 28.4601, lng: 77.0302 });
  });

  test('an offer message parses to an offer', () => {
    expect(parseSocketMessage('{"kind":"offer","order_id":9,"expires_at":1760000030000}')).toEqual({ kind: 'offer', orderId: 9, expiresAt: 1760000030000 });
  });

  test('garbage and unknown shapes parse to null', () => {
    expect(parseSocketMessage('nope')).toBeNull();
    expect(parseSocketMessage('{"hello":"world"}')).toBeNull();
    expect(parseSocketMessage('{"lat":"x","lng":1}')).toBeNull();
    expect(parseSocketMessage('42')).toBeNull();
  });
});
