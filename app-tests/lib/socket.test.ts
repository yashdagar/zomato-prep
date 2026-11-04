import { nextDelay, socketUrl } from '@app/useSocket';

describe('W5 B6 reconnect backoff', () => {
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

  test('the socket url swaps http for ws and carries the token', () => {
    expect(socketUrl('http://192.168.1.5:8080', 'abc')).toBe('ws://192.168.1.5:8080/v1/ws?token=abc');
  });

  test('an https api becomes a wss socket', () => {
    expect(socketUrl('https://api.example.com', 'abc')).toBe('wss://api.example.com/v1/ws?token=abc');
  });

  test('a trailing slash on the base url does not double up', () => {
    expect(socketUrl('http://localhost:8080/', 'abc')).toBe('ws://localhost:8080/v1/ws?token=abc');
  });

  test('the token is url encoded', () => {
    expect(socketUrl('http://localhost:8080', 'a b&c=d')).toBe('ws://localhost:8080/v1/ws?token=a%20b%26c%3Dd');
  });
});
