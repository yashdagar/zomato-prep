import { ApiClient, ApiError } from '@app/api';
import { authErrorMessage, login, logout, passwordProblem, signup } from '@app/authApi';
import { body, fakeFetch, header, json, memStore } from './fakes';

const tokens = { access_token: 'A1', refresh_token: 'R1', token_type: 'Bearer', expires_in: 900 };

describe('W4 B2 login and signup', () => {
  test('login posts the credentials and stores the returned pair', async () => {
    const calls = fakeFetch(() => json(200, tokens));
    const store = memStore(null);
    await login(new ApiClient('http://api', store), store, { email: 'a@b.c', password: 'secret123' });
    expect(calls[0].url).toBe('http://api/v1/auth/login');
    expect(calls[0].init.method).toBe('POST');
    expect(body(calls[0])).toEqual({ email: 'a@b.c', password: 'secret123' });
    expect(store.current).toEqual({ access: 'A1', refresh: 'R1' });
  });

  test('signup posts to the signup endpoint and stores the returned pair', async () => {
    const calls = fakeFetch(() => json(201, tokens));
    const store = memStore(null);
    await signup(new ApiClient('http://api', store), store, { email: 'a@b.c', password: 'secret123' });
    expect(calls[0].url).toBe('http://api/v1/auth/signup');
    expect(store.current).toEqual({ access: 'A1', refresh: 'R1' });
  });

  test('a wrong password rejects with a 401 and never tries a refresh', async () => {
    const calls = fakeFetch(() => json(401, { error: 'invalid credentials' }));
    const store = memStore({ access: 'stale', refresh: 'stale-r' });
    const err = await login(new ApiClient('http://api', store), store, { email: 'a@b.c', password: 'nope' }).catch(e => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(401);
    expect(calls.map(c => c.url)).toEqual(['http://api/v1/auth/login']);
  });

  test('login does not send a stale bearer token', async () => {
    const calls = fakeFetch(() => json(200, tokens));
    const store = memStore({ access: 'stale', refresh: 'stale-r' });
    await login(new ApiClient('http://api', store), store, { email: 'a@b.c', password: 'secret123' });
    expect(header(calls[0], 'Authorization')).toBeNull();
  });

  test('a failed login leaves the stored tokens untouched', async () => {
    fakeFetch(() => json(401, {}));
    const store = memStore(null);
    await login(new ApiClient('http://api', store), store, { email: 'a@b.c', password: 'nope' }).catch(() => undefined);
    expect(store.current).toBeNull();
  });

  test('logout clears the stored pair', async () => {
    const store = memStore({ access: 'A', refresh: 'R' });
    await logout(store);
    expect(store.current).toBeNull();
  });

  test('error messages are specific for wrong credentials, a taken email, rate limits and no network', () => {
    expect(authErrorMessage(new ApiError(401, null))).toMatch(/wrong email or password/i);
    expect(authErrorMessage(new ApiError(409, null))).toMatch(/already has an account/i);
    expect(authErrorMessage(new ApiError(429, null, 7))).toMatch(/7 s/);
    expect(authErrorMessage(new TypeError('Network request failed'))).toMatch(/cannot reach the server/i);
  });

  test('a 400 shows the server error text', () => {
    expect(authErrorMessage(new ApiError(400, { error: 'email is invalid' }))).toBe('email is invalid');
  });

  test('passwords must be 8 characters to 72 bytes, counted in bytes', () => {
    expect(passwordProblem('short')).not.toBeNull();
    expect(passwordProblem('long enough')).toBeNull();
    expect(passwordProblem('a'.repeat(72))).toBeNull();
    expect(passwordProblem('a'.repeat(73))).not.toBeNull();
    expect(passwordProblem('é'.repeat(37))).not.toBeNull();
  });
});
