import { bearing, etaMinutes, haversineKm, interpolate } from '@app/eta';

const dlf = { lat: 28.4595, lng: 77.0266 };

describe('W5 O3 rider ETA and marker interpolation', () => {
  test('the distance between a point and itself is zero', () => {
    expect(haversineKm(dlf, dlf)).toBe(0);
  });

  test('one degree of latitude is about 111 km', () => {
    expect(haversineKm({ lat: 0, lng: 0 }, { lat: 1, lng: 0 })).toBeCloseTo(111.19, 1);
  });

  test('the distance is the same in both directions', () => {
    const a = { lat: 28.4595, lng: 77.0266 };
    const b = { lat: 28.5355, lng: 77.391 };
    expect(haversineKm(a, b)).toBeCloseTo(haversineKm(b, a), 9);
    expect(haversineKm(a, b)).toBeGreaterThan(30);
    expect(haversineKm(a, b)).toBeLessThan(45);
  });

  test('the eta rounds up to whole minutes at the given speed', () => {
    expect(etaMinutes(5, 30)).toBe(10);
    expect(etaMinutes(5.1, 30)).toBe(11);
  });

  test('the default speed is 20 km/h', () => {
    expect(etaMinutes(10)).toBe(30);
  });

  test('a rider who has arrived still shows one minute', () => {
    expect(etaMinutes(0)).toBe(1);
    expect(etaMinutes(0.001, 20)).toBe(1);
  });

  test('a non-positive speed gives no eta', () => {
    expect(etaMinutes(5, 0)).toBeNull();
    expect(etaMinutes(5, -3)).toBeNull();
  });

  test('interpolation at zero and one gives the endpoints', () => {
    const a = { lat: 10, lng: 20 };
    const b = { lat: 12, lng: 24 };
    expect(interpolate(a, b, 0)).toEqual(a);
    expect(interpolate(a, b, 1)).toEqual(b);
  });

  test('interpolation halfway is the midpoint', () => {
    expect(interpolate({ lat: 10, lng: 20 }, { lat: 12, lng: 24 }, 0.5)).toEqual({ lat: 11, lng: 22 });
  });

  test('interpolation clamps t to the segment', () => {
    const a = { lat: 10, lng: 20 };
    const b = { lat: 12, lng: 24 };
    expect(interpolate(a, b, -1)).toEqual(a);
    expect(interpolate(a, b, 7)).toEqual(b);
  });

  test('bearing points north, east, south and west', () => {
    const o = { lat: 0, lng: 0 };
    expect(bearing(o, { lat: 1, lng: 0 })).toBeCloseTo(0, 5);
    expect(bearing(o, { lat: 0, lng: 1 })).toBeCloseTo(90, 5);
    expect(bearing(o, { lat: -1, lng: 0 })).toBeCloseTo(180, 5);
    expect(bearing(o, { lat: 0, lng: -1 })).toBeCloseTo(270, 5);
  });

  test('bearing stays between 0 and 360', () => {
    const b = bearing({ lat: 28.46, lng: 77.03 }, { lat: 28.4, lng: 76.9 });
    expect(b).toBeGreaterThanOrEqual(0);
    expect(b).toBeLessThan(360);
  });
});
