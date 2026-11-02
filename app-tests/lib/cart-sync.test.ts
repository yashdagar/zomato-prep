import { useCart, type CartLine } from '@app/cart';
import { addOptimistic, setQtyOptimistic, type CartApi } from '@app/cartSync';

const line = (over: Partial<CartLine> = {}): CartLine => ({ itemId: 1, size: 'M', chosen: {}, qty: 1, unitPaise: 29900, description: 'Farmhouse (M)', ...over });

type Snapshot = { restaurantId: number | null; lines: CartLine[] };

function fakeApi() {
  const saved: Snapshot[] = [];
  let pending: { resolve(): void; reject(e: Error): void } | null = null;
  const api: CartApi = {
    saveCart(cart) {
      saved.push({ restaurantId: cart.restaurantId, lines: cart.lines });
      return new Promise<void>((resolve, reject) => { pending = { resolve, reject }; });
    },
  };
  return {
    api,
    saved,
    resolve: () => pending?.resolve(),
    reject: (e: Error) => pending?.reject(e),
  };
}

beforeEach(() => useCart.getState().clear());

describe('W4 O2 Optimistic cart updates', () => {
  test('adding a line shows in the store before the server answers', async () => {
    const f = fakeApi();
    const done = addOptimistic(f.api, 5, line());
    expect(useCart.getState().lines).toHaveLength(1);
    f.resolve();
    await done;
  });

  test('the new cart is sent to the server', async () => {
    const f = fakeApi();
    const done = addOptimistic(f.api, 5, line({ itemId: 3 }));
    f.resolve();
    await done;
    expect(f.saved).toHaveLength(1);
    expect(f.saved[0].restaurantId).toBe(5);
    expect(f.saved[0].lines.map(l => l.itemId)).toEqual([3]);
  });

  test('a successful add resolves ok and keeps the line', async () => {
    const f = fakeApi();
    const done = addOptimistic(f.api, 5, line());
    f.resolve();
    await expect(done).resolves.toBe('ok');
    expect(useCart.getState().lines).toHaveLength(1);
    expect(useCart.getState().idempotencyKey).toBeTruthy();
  });

  test('a rejected add rolls back the lines, the restaurant and the key and rethrows', async () => {
    const f = fakeApi();
    const done = addOptimistic(f.api, 5, line());
    f.reject(new Error('offline'));
    await expect(done).rejects.toThrow('offline');
    const s = useCart.getState();
    expect(s.lines).toEqual([]);
    expect(s.restaurantId).toBeNull();
    expect(s.idempotencyKey).toBeNull();
  });

  test('a rejected add keeps the lines that were there before', async () => {
    useCart.getState().add(5, line({ itemId: 1 }));
    const key = useCart.getState().idempotencyKey;
    const f = fakeApi();
    const done = addOptimistic(f.api, 5, line({ itemId: 2 }));
    f.reject(new Error('offline'));
    await done.catch(() => undefined);
    expect(useCart.getState().lines.map(l => l.itemId)).toEqual([1]);
    expect(useCart.getState().idempotencyKey).toBe(key);
  });

  test('a line from another restaurant is refused without calling the server', async () => {
    useCart.getState().add(5, line());
    const f = fakeApi();
    await expect(addOptimistic(f.api, 6, line({ itemId: 9 }))).resolves.toBe('other-restaurant');
    expect(f.saved).toEqual([]);
    expect(useCart.getState().lines).toHaveLength(1);
  });

  test('changing a quantity shows immediately and is sent to the server', async () => {
    useCart.getState().add(5, line({ qty: 1 }));
    const f = fakeApi();
    const done = setQtyOptimistic(f.api, 0, 4);
    expect(useCart.getState().lines[0].qty).toBe(4);
    f.resolve();
    await done;
    expect(f.saved[0].lines[0].qty).toBe(4);
  });

  test('a rejected quantity change restores the old quantity', async () => {
    useCart.getState().add(5, line({ qty: 2 }));
    const f = fakeApi();
    const done = setQtyOptimistic(f.api, 0, 7);
    f.reject(new Error('500'));
    await expect(done).rejects.toThrow('500');
    expect(useCart.getState().lines[0].qty).toBe(2);
  });

  test('a rejected removal of the last line restores the line, the restaurant and the key', async () => {
    useCart.getState().add(5, line());
    const key = useCart.getState().idempotencyKey;
    const f = fakeApi();
    const done = setQtyOptimistic(f.api, 0, 0);
    expect(useCart.getState().lines).toEqual([]);
    f.reject(new Error('offline'));
    await done.catch(() => undefined);
    const s = useCart.getState();
    expect(s.lines).toHaveLength(1);
    expect(s.restaurantId).toBe(5);
    expect(s.idempotencyKey).toBe(key);
  });

  test('a rollback does not overwrite a newer change made while the request was in flight', async () => {
    const f = fakeApi();
    const done = addOptimistic(f.api, 5, line({ itemId: 1 }));
    useCart.getState().add(5, line({ itemId: 2 }));
    f.reject(new Error('offline'));
    await done.catch(() => undefined);
    expect(useCart.getState().lines.map(l => l.itemId)).toContain(2);
  });
});
