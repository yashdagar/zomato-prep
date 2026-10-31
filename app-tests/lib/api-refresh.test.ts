import { ApiClient, ApiError } from '@app/api';
import { fakeFetch, json, memStore } from './fakes';

describe('W4 B2 ApiClient refresh failure', () => {
  test('a rejected refresh clears the stored tokens and surfaces the 401', async () => {
    const store = memStore({ access: 'old', refresh: 'revoked' });
    fakeFetch(call => (call.url.endsWith('/v1/auth/refresh') ? json(401, { error: 'invalid refresh token' }) : json(401, {})));
    const err = (await new ApiClient('http://api', store).request('/v1/me').catch(e => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(401);
    expect(store.current).toBeNull();
  });

  test('a refresh that fails on the network keeps the tokens and reports the network error', async () => {
    const store = memStore({ access: 'old', refresh: 'R1' });
    fakeFetch(call => {
      if (call.url.endsWith('/v1/auth/refresh')) throw new TypeError('Network request failed');
      return json(401, {});
    });
    const err = await new ApiClient('http://api', store).request('/v1/me').catch(e => e);
    expect(err).toBeInstanceOf(TypeError);
    expect(store.current).toEqual({ access: 'old', refresh: 'R1' });
  });

  test('the refresh sends the stored refresh token as JSON', async () => {
    const store = memStore({ access: 'old', refresh: 'R1' });
    const calls = fakeFetch(call => {
      if (call.url.endsWith('/v1/auth/refresh')) return json(200, { access_token: 'new', refresh_token: 'R2' });
      return new Headers(call.init.headers).get('Authorization') === 'Bearer new' ? json(200, {}) : json(401, {});
    });
    await new ApiClient('http://api', store).request('/v1/me');
    const refresh = calls.find(c => c.url.endsWith('/v1/auth/refresh'))!;
    expect(refresh.init.method).toBe('POST');
    expect(JSON.parse(String(refresh.init.body))).toEqual({ refresh_token: 'R1' });
  });

  test('a 204 resolves to undefined without parsing a body', async () => {
    fakeFetch(() => new Response(null, { status: 204 }));
    await expect(new ApiClient('http://api', memStore(null)).request('/x', { method: 'DELETE' })).resolves.toBeUndefined();
  });

  test('a network failure on the request itself is not reported as an ApiError', async () => {
    fakeFetch(() => { throw new TypeError('Network request failed'); });
    const err = await new ApiClient('http://api', memStore(null)).request('/x').catch(e => e);
    expect(err).toBeInstanceOf(TypeError);
    expect(err).not.toBeInstanceOf(ApiError);
  });

  test('a request without a stored pair sends no Authorization header and never refreshes', async () => {
    const calls = fakeFetch(() => json(401, {}));
    await new ApiClient('http://api', memStore(null)).request('/v1/me').catch(() => undefined);
    expect(calls).toHaveLength(1);
    expect(new Headers(calls[0].init.headers).get('Authorization')).toBeNull();
  });
});
