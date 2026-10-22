import { nextDelay } from '@app/useSocket';

describe('W5 L6 reconnect backoff', () => {
  test('doubles from one second', () => {
    const noJitter = () => 0.5;
    expect(nextDelay(0, noJitter)).toBe(1000);
    expect(nextDelay(1, noJitter)).toBe(2000);
    expect(nextDelay(3, noJitter)).toBe(8000);
  });

  test('caps at 30 seconds before jitter', () => {
    expect(nextDelay(10, () => 0.5)).toBe(30000);
    expect(nextDelay(20, () => 0.5)).toBe(30000);
  });

  test('jitter spreads attempts between half and one and a half of the base', () => {
    expect(nextDelay(2, () => 0)).toBe(2000);
    expect(nextDelay(2, () => 0.999)).toBeCloseTo(5996, 0);
  });
});
