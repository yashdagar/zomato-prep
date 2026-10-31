import type { Tokens, TokenStore } from '@app/api';

export type Call = { url: string; init: RequestInit };

export function memStore(initial: Tokens | null): TokenStore & { current: Tokens | null } {
  return {
    current: initial,
    async get() { return this.current; },
    async set(t) { this.current = t; },
  };
}

export function fakeFetch(script: (call: Call, n: number) => Response | Promise<Response>) {
  const calls: Call[] = [];
  (globalThis as unknown as { fetch: unknown }).fetch = jest.fn(async (url: string, init: RequestInit = {}) => {
    const call = { url, init };
    calls.push(call);
    return script(call, calls.length);
  });
  return calls;
}

export const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });

export const header = (call: Call, name: string) => new Headers(call.init.headers).get(name);
export const body = (call: Call) => JSON.parse(String(call.init.body));
