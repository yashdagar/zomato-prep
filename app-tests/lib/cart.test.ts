import { useCart, type CartLine } from '@app/cart';

const line = (over: Partial<CartLine> = {}): CartLine => ({ itemId: 1, size: 'M', chosen: {}, qty: 1, unitPaise: 29900, description: 'Farmhouse (M)', ...over });

beforeEach(() => useCart.getState().clear());

describe('W4 B6 cart store', () => {
  test('adding the first line creates an idempotency key that survives further adds', () => {
    expect(useCart.getState().idempotencyKey).toBeNull();
    expect(useCart.getState().add(5, line())).toBe('ok');
    const key = useCart.getState().idempotencyKey;
    expect(key).toBeTruthy();
    useCart.getState().add(5, line({ itemId: 2 }));
    expect(useCart.getState().idempotencyKey).toBe(key);
  });

  test('refuses a line from another restaurant while the cart is not empty', () => {
    useCart.getState().add(5, line());
    expect(useCart.getState().add(6, line({ itemId: 9 }))).toBe('other-restaurant');
    expect(useCart.getState().lines).toHaveLength(1);
  });

  test('total multiplies unit price by qty across lines', () => {
    useCart.getState().add(5, line({ qty: 2, unitPaise: 10000 }));
    useCart.getState().add(5, line({ itemId: 2, qty: 1, unitPaise: 5000 }));
    expect(useCart.getState().total()).toBe(25000);
  });

  test('setQty to zero removes the line', () => {
    useCart.getState().add(5, line());
    useCart.getState().setQty(0, 0);
    expect(useCart.getState().lines).toHaveLength(0);
  });

  test('clear resets restaurant, lines and the key', () => {
    useCart.getState().add(5, line());
    useCart.getState().clear();
    const s = useCart.getState();
    expect(s.restaurantId).toBeNull();
    expect(s.lines).toEqual([]);
    expect(s.idempotencyKey).toBeNull();
  });
});
