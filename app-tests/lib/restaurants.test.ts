import { ApiClient } from '@app/api';
import { flatten, nextCursor } from '@app/pages';
import { fetchRestaurants, openLabel, restaurantsPath } from '@app/restaurants';
import { fakeFetch, json, memStore } from './fakes';

const near = { lat: 28.46, lng: 77.03, radiusKm: 5 };
const params = (path: string) => new URL('http://x' + path).searchParams;

describe('W4 B3 restaurant list', () => {
  test('the first page asks for 20 restaurants near the point without a cursor', () => {
    const q = params(restaurantsPath(near, ''));
    expect(restaurantsPath(near, '').startsWith('/v1/restaurants?')).toBe(true);
    expect(q.get('lat')).toBe('28.46');
    expect(q.get('lng')).toBe('77.03');
    expect(q.get('radius_km')).toBe('5');
    expect(q.get('limit')).toBe('20');
    expect(q.has('cursor')).toBe(false);
  });

  test('later pages pass the cursor through, URL-encoded', () => {
    expect(params(restaurantsPath(near, 'eyJpZCI6NDJ9+/=')).get('cursor')).toBe('eyJpZCI6NDJ9+/=');
  });

  test('an empty next_cursor means there are no more pages', () => {
    expect(nextCursor({ items: [], next_cursor: '' })).toBeUndefined();
    expect(nextCursor({ items: [], next_cursor: 'abc' })).toBe('abc');
  });

  test('pages flatten into one list in order', () => {
    expect(flatten([{ items: [1, 2], next_cursor: 'a' }, { items: [3], next_cursor: '' }])).toEqual([1, 2, 3]);
    expect(flatten(undefined)).toEqual([]);
  });

  test('fetching a page goes through the API client', async () => {
    const calls = fakeFetch(() => json(200, { items: [{ id: 1, name: 'Pizza Hut', lat: 28.46, lng: 77.03 }], next_cursor: 'c1' }));
    const page = await fetchRestaurants(new ApiClient('http://api', memStore({ access: 'A', refresh: 'R' })), near, 'c0');
    expect(calls[0].url).toBe('http://api' + restaurantsPath(near, 'c0'));
    expect(new Headers(calls[0].init.headers).get('Authorization')).toBe('Bearer A');
    expect(page.next_cursor).toBe('c1');
  });

  test('the open pill appears only when the API says whether the restaurant is open', () => {
    expect(openLabel({ id: 1, name: 'a', lat: 0, lng: 0, is_open: true })).toBe('Open');
    expect(openLabel({ id: 1, name: 'a', lat: 0, lng: 0, is_open: false })).toBe('Closed');
    expect(openLabel({ id: 1, name: 'a', lat: 0, lng: 0 })).toBeNull();
  });
});
