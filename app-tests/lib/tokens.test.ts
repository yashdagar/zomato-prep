import { SecureTokenStore } from '@app/tokens';
import * as SecureStore from 'expo-secure-store';

let n = 0;
const freshKey = () => `test.tokens.${++n}`;

describe('W4 B2 SecureTokenStore', () => {
  test('returns null when nothing has been stored', async () => {
    await expect(new SecureTokenStore(freshKey()).get()).resolves.toBeNull();
  });

  test('a stored pair survives a new store instance, as after an app restart', async () => {
    const key = freshKey();
    await new SecureTokenStore(key).set({ access: 'A', refresh: 'R' });
    await expect(new SecureTokenStore(key).get()).resolves.toEqual({ access: 'A', refresh: 'R' });
  });

  test('setting null deletes the pair from secure storage', async () => {
    const key = freshKey();
    const store = new SecureTokenStore(key);
    await store.set({ access: 'A', refresh: 'R' });
    await store.set(null);
    await expect(SecureStore.getItemAsync(key)).resolves.toBeNull();
    await expect(new SecureTokenStore(key).get()).resolves.toBeNull();
  });

  test('a corrupt stored value reads as signed out instead of throwing', async () => {
    const key = freshKey();
    await SecureStore.setItemAsync(key, '{not json');
    await expect(new SecureTokenStore(key).get()).resolves.toBeNull();
  });

  test('a stored value without both tokens reads as signed out', async () => {
    const key = freshKey();
    await SecureStore.setItemAsync(key, JSON.stringify({ access: 'A' }));
    await expect(new SecureTokenStore(key).get()).resolves.toBeNull();
  });

  test('subscribers hear about sign-in and sign-out', async () => {
    const store = new SecureTokenStore(freshKey());
    const seen: unknown[] = [];
    store.subscribe(t => seen.push(t));
    await store.set({ access: 'A', refresh: 'R' });
    await store.set(null);
    expect(seen).toEqual([{ access: 'A', refresh: 'R' }, null]);
  });

  test('an unsubscribed listener is not called again', async () => {
    const store = new SecureTokenStore(freshKey());
    const listener = jest.fn();
    const unsubscribe = store.subscribe(listener);
    unsubscribe();
    await store.set({ access: 'A', refresh: 'R' });
    expect(listener).not.toHaveBeenCalled();
  });
});
