import AsyncStorage from '@react-native-async-storage/async-storage';
import { useCart, type CartLine } from '@app/cart';

const line = (over: Partial<CartLine> = {}): CartLine => ({ itemId: 1, size: 'M', chosen: {}, qty: 1, unitPaise: 29900, description: 'Farmhouse (M)', ...over });
const saved = async () => JSON.parse((await AsyncStorage.getItem('cart')) ?? 'null');

beforeEach(() => useCart.getState().clear());

describe('W4 B6 cart persistence', () => {
  test('the cart, its restaurant and its key are written to storage', async () => {
    useCart.getState().add(5, line());
    const s = await saved();
    expect(s.state.restaurantId).toBe(5);
    expect(s.state.lines).toHaveLength(1);
    expect(s.state.idempotencyKey).toBe(useCart.getState().idempotencyKey);
  });

  test('a cart restored from storage keeps its idempotency key', async () => {
    await AsyncStorage.setItem('cart', JSON.stringify({ state: { restaurantId: 7, lines: [line()], idempotencyKey: 'key-from-before' }, version: 0 }));
    await useCart.persist.rehydrate();
    expect(useCart.getState().restaurantId).toBe(7);
    expect(useCart.getState().idempotencyKey).toBe('key-from-before');
  });

  test('replacing the cart with another restaurant starts a new key', () => {
    useCart.getState().add(5, line());
    const first = useCart.getState().idempotencyKey;
    useCart.getState().replaceWith(6, line({ itemId: 9 }));
    const s = useCart.getState();
    expect(s.restaurantId).toBe(6);
    expect(s.lines.map(l => l.itemId)).toEqual([9]);
    expect(s.idempotencyKey).not.toBe(first);
  });

  test('removing the last line empties the cart and forgets the key', () => {
    useCart.getState().add(5, line());
    useCart.getState().setQty(0, 0);
    expect(useCart.getState().restaurantId).toBeNull();
    expect(useCart.getState().idempotencyKey).toBeNull();
  });

  test('changing a quantity keeps the key, so a retry after editing stays one order', () => {
    useCart.getState().add(5, line());
    const key = useCart.getState().idempotencyKey;
    useCart.getState().setQty(0, 3);
    expect(useCart.getState().lines[0].qty).toBe(3);
    expect(useCart.getState().idempotencyKey).toBe(key);
  });

  test('two separate carts get different keys', () => {
    useCart.getState().add(5, line());
    const first = useCart.getState().idempotencyKey;
    useCart.getState().clear();
    useCart.getState().add(5, line());
    expect(useCart.getState().idempotencyKey).not.toBe(first);
  });
});
