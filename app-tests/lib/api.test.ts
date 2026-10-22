import { ApiClient, ApiError, type TokenStore, type Tokens } from '@app/api';

type Call = { url: string; init: RequestInit };

function memStore(initial: Tokens | null): TokenStore & { current: Tokens | null } {
  return {
    current: initial,
    async get() { return this.current; },
    async set(t) { this.current = t; },
  };
}

function fakeFetch(script: (call: Call, n: number) => Response | Promise<Response>) {
  const calls: Call[] = [];
  const fn = jest.fn(async (url: string, init: RequestInit = {}) => {
    const call = { url, init };
    calls.push(call);
    return script(call, calls.length);
  });
  (globalThis as unknown as { fetch: unknown }).fetch = fn;
  return calls;
}

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });

describe('W4 B2 ApiClient', () => {
  test('sends the bearer token and parses JSON', async () => {
    const calls = fakeFetch(() => json(200, { ok: true }));
    const api = new ApiClient('http://api', memStore({ access: 'A', refresh: 'R' }));
    await expect(api.request('/v1/me')).resolves.toEqual({ ok: true });
    expect(calls[0].url).toBe('http://api/v1/me');
    expect(new Headers(calls[0].init.headers).get('Authorization')).toBe('Bearer A');
  });

  test('on 401 refreshes once, stores the new pair, and retries the request', async () => {
    const store = memStore({ access: 'old', refresh: 'R1' });
    const calls = fakeFetch(call => {
      if (call.url.endsWith('/v1/auth/refresh')) return json(200, { access_token: 'new', refresh_token: 'R2' });
      const auth = new Headers(call.init.headers).get('Authorization');
      return auth === 'Bearer new' ? json(200, { me: 1 }) : json(401, { error: 'unauthorized' });
    });
    const api = new ApiClient('http://api', store);
    await expect(api.request('/v1/me')).resolves.toEqual({ me: 1 });
    expect(store.current).toEqual({ access: 'new', refresh: 'R2' });
    expect(calls.filter(c => c.url.endsWith('/v1/auth/refresh'))).toHaveLength(1);
    expect(calls).toHaveLength(3);
  });

  test('parallel 401s share one refresh (single in-flight promise)', async () => {
    const store = memStore({ access: 'old', refresh: 'R1' });
    const calls = fakeFetch(async call => {
      if (call.url.endsWith('/v1/auth/refresh')) {
        await new Promise(r => setTimeout(r, 20));
        return json(200, { access_token: 'new', refresh_token: 'R2' });
      }
      const auth = new Headers(call.init.headers).get('Authorization');
      return auth === 'Bearer new' ? json(200, { ok: 1 }) : json(401, {});
    });
    const api = new ApiClient('http://api', store);
    await Promise.all([api.request('/a'), api.request('/b'), api.request('/c')]);
    expect(calls.filter(c => c.url.endsWith('/v1/auth/refresh'))).toHaveLength(1);
  });

  test('a second 401 after refresh throws instead of looping', async () => {
    fakeFetch(call => (call.url.endsWith('/v1/auth/refresh') ? json(200, { access_token: 'n', refresh_token: 'r' }) : json(401, {})));
    const api = new ApiClient('http://api', memStore({ access: 'a', refresh: 'r' }));
    await expect(api.request('/x')).rejects.toBeInstanceOf(ApiError);
  });

  test('429 becomes an ApiError carrying Retry-After', async () => {
    fakeFetch(() => json(429, { error: 'rate_limited' }, { 'Retry-After': '3' }));
    const api = new ApiClient('http://api', memStore(null));
    const err = (await api.request('/x').catch(e => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(429);
    expect(err.retryAfterSec).toBe(3);
  });

  test('other errors expose status and body', async () => {
    fakeFetch(() => json(400, { error: 'bad cursor' }));
    const api = new ApiClient('http://api', memStore(null));
    const err = (await api.request('/x').catch(e => e)) as ApiError;
    expect(err.status).toBe(400);
    expect(err.body).toEqual({ error: 'bad cursor' });
  });

  test('POST sends a JSON body and extra headers', async () => {
    const calls = fakeFetch(() => json(201, { id: 7 }));
    const api = new ApiClient('http://api', memStore(null));
    await api.request('/v1/orders', { method: 'POST', body: { restaurant_id: 1 }, headers: { 'Idempotency-Key': 'k1' } });
    expect(calls[0].init.method).toBe('POST');
    expect(calls[0].init.body).toBe(JSON.stringify({ restaurant_id: 1 }));
    expect(new Headers(calls[0].init.headers).get('Idempotency-Key')).toBe('k1');
  });
});
