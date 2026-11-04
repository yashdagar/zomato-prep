import { locationBody, offerSecondsLeft, parseOffer, riderAction } from '@app/rider';

describe('W5 B6 rider mode', () => {
  test('the countdown rounds up so it reaches zero only at expiry', () => {
    expect(offerSecondsLeft(30_000, 0)).toBe(30);
    expect(offerSecondsLeft(30_000, 100)).toBe(30);
    expect(offerSecondsLeft(30_000, 29_001)).toBe(1);
    expect(offerSecondsLeft(30_000, 30_000)).toBe(0);
  });

  test('the countdown never goes below zero', () => {
    expect(offerSecondsLeft(30_000, 45_000)).toBe(0);
  });

  test('a ready order is picked up and a picked up order is delivered', () => {
    expect(riderAction('READY')).toEqual({ label: 'Picked up', event: 'PICK_UP' });
    expect(riderAction('PICKED_UP')).toEqual({ label: 'Delivered', event: 'DELIVER' });
  });

  test('no other status has a rider action', () => {
    for (const s of ['PLACED', 'ACCEPTED', 'PREPARING', 'DELIVERED', 'CANCELLED', 'REJECTED']) {
      expect(riderAction(s)).toBeNull();
    }
  });

  test('the location body uses lat and lng from the device coordinates', () => {
    expect(locationBody({ coords: { latitude: 28.4601, longitude: 77.0302 } })).toEqual({ lat: 28.4601, lng: 77.0302 });
  });

  test('an offer message becomes an offer with its expiry in milliseconds', () => {
    expect(parseOffer('{"kind":"offer","order_id":9,"expires_at":1760000030000}')).toEqual({ orderId: 9, expiresAt: 1760000030000 });
  });

  test('messages that are not offers are not offers', () => {
    expect(parseOffer('{"order_id":9,"to":"READY"}')).toBeNull();
    expect(parseOffer('{"lat":1,"lng":2}')).toBeNull();
    expect(parseOffer('not json')).toBeNull();
  });

  test('an offer without an order id or expiry is rejected', () => {
    expect(parseOffer('{"kind":"offer","order_id":9}')).toBeNull();
    expect(parseOffer('{"kind":"offer","expires_at":1}')).toBeNull();
  });
});
