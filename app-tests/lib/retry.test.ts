import { ApiClient, ApiError } from '@app/api';
import { requestWithRetry } from '@app/retry';
import { fakeFetch, json, memStore } from './fakes';

const client = () => new ApiClient('http://api', memStore(null));

function recorder() {
  const sleeps: number[] = [];
  return { sleeps, sleep: async (ms: number) => { sleeps.push(ms); } };
}

describe('W4 O1 Retry with backoff and jitter', () => {
  test('a first-try success returns the body without sleeping', async () => {
    fakeFetch(() => json(200, { ok: true }));
    const r = recorder();
    await expect(requestWithRetry(client(), '/v1/me', {}, { sleep: r.sleep })).resolves.toEqual({ ok: true });
    expect(r.sleeps).toEqual([]);
  });

  test('a GET that gets a 503 is retried and the later success is returned', async () => {
    const calls = fakeFetch((_c, n) => (n < 3 ? json(503, { error: 'unavailable' }) : json(200, { ok: 1 })));
    const r = recorder();
    await expect(requestWithRetry(client(), '/v1/restaurants', {}, { sleep: r.sleep, random: () => 1 })).resolves.toEqual({ ok: 1 });
    expect(calls).toHaveLength(3);
    expect(r.sleeps).toHaveLength(2);
  });

  test('a GET that fails with a network error is retried', async () => {
    const calls = fakeFetch((_c, n) => {
      if (n === 1) throw new TypeError('Network request failed');
      return json(200, { ok: 1 });
    });
    const r = recorder();
    await expect(requestWithRetry(client(), '/v1/restaurants', {}, { sleep: r.sleep })).resolves.toEqual({ ok: 1 });
    expect(calls).toHaveLength(2);
  });

  test('a 4xx answer is thrown at once and never retried', async () => {
    const calls = fakeFetch(() => json(400, { error: 'bad cursor' }));
    const r = recorder();
    const err = (await requestWithRetry(client(), '/v1/restaurants', {}, { sleep: r.sleep }).catch(e => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(400);
    expect(calls).toHaveLength(1);
    expect(r.sleeps).toEqual([]);
  });

  test('a POST is never retried even when the server answers 503', async () => {
    const calls = fakeFetch(() => json(503, {}));
    const r = recorder();
    const err = (await requestWithRetry(client(), '/v1/orders', { method: 'POST', body: { restaurant_id: 1 } }, { sleep: r.sleep }).catch(e => e)) as ApiError;
    expect(err.status).toBe(503);
    expect(calls).toHaveLength(1);
    expect(r.sleeps).toEqual([]);
  });

  test('it gives up after maxAttempts and throws the last error', async () => {
    const calls = fakeFetch(() => json(500, { error: 'boom' }));
    const r = recorder();
    const err = (await requestWithRetry(client(), '/x', {}, { maxAttempts: 4, sleep: r.sleep }).catch(e => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(500);
    expect(calls).toHaveLength(4);
  });

  test('it does not sleep after the final failed attempt', async () => {
    fakeFetch(() => json(502, {}));
    const r = recorder();
    await requestWithRetry(client(), '/x', {}, { maxAttempts: 3, sleep: r.sleep }).catch(() => undefined);
    expect(r.sleeps).toHaveLength(2);
  });

  test('delays double each attempt from baseMs when random returns 1', async () => {
    fakeFetch(() => json(503, {}));
    const r = recorder();
    await requestWithRetry(client(), '/x', {}, { maxAttempts: 4, baseMs: 100, maxMs: 10_000, sleep: r.sleep, random: () => 1 }).catch(() => undefined);
    expect(r.sleeps).toEqual([100, 200, 400]);
  });

  test('jitter scales each delay by the random value', async () => {
    fakeFetch(() => json(503, {}));
    const r = recorder();
    await requestWithRetry(client(), '/x', {}, { maxAttempts: 4, baseMs: 100, maxMs: 10_000, sleep: r.sleep, random: () => 0.5 }).catch(() => undefined);
    expect(r.sleeps).toEqual([50, 100, 200]);
  });

  test('delays are capped at maxMs before jitter is applied', async () => {
    fakeFetch(() => json(503, {}));
    const r = recorder();
    await requestWithRetry(client(), '/x', {}, { maxAttempts: 5, baseMs: 100, maxMs: 300, sleep: r.sleep, random: () => 1 }).catch(() => undefined);
    expect(r.sleeps).toEqual([100, 200, 300, 300]);
  });

  test('Retry-After on the error replaces the computed delay', async () => {
    let n = 0;
    const api = {
      request: async <T>(): Promise<T> => {
        if (++n === 1) throw new ApiError(503, null, 2);
        return { ok: true } as T;
      },
    };
    const r = recorder();
    await expect(requestWithRetry(api, '/x', {}, { sleep: r.sleep, random: () => 0.1, baseMs: 100 })).resolves.toEqual({ ok: true });
    expect(r.sleeps).toEqual([2000]);
  });

  test('an explicit GET method is retried like the default', async () => {
    const calls = fakeFetch((_c, n) => (n === 1 ? json(500, {}) : json(200, { ok: 1 })));
    const r = recorder();
    await requestWithRetry(client(), '/x', { method: 'GET' }, { sleep: r.sleep });
    expect(calls).toHaveLength(2);
  });
});
