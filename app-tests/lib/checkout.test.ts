import { ApiClient, ApiError } from '@app/api';
import { checkoutErrorMessage, placeOrder, type CartSnapshot } from '@app/checkout';
import { body, fakeFetch, header, json, memStore } from './fakes';

const cart = (): CartSnapshot => ({
  restaurantId: 5,
  idempotencyKey: 'key-1',
  lines: [{ itemId: 1, size: 'M', chosen: { '10': [101] }, qty: 2, unitPaise: 39900, description: 'Farmhouse (M)' }],
});
const placed = { id: 77, status: 'PLACED', total_paise: 79800, restaurant_id: 5 };
const api = () => new ApiClient('http://api', memStore({ access: 'A', refresh: 'R' }));

describe('W4 B7 checkout', () => {
  test('placing an order sends the cart key as the Idempotency-Key header', async () => {
    const calls = fakeFetch(() => json(201, placed));
    await placeOrder(api(), cart(), () => undefined);
    expect(calls[0].url).toBe('http://api/v1/orders');
    expect(calls[0].init.method).toBe('POST');
    expect(header(calls[0], 'Idempotency-Key')).toBe('key-1');
  });

  test('the body lists the restaurant and each line without client prices', async () => {
    const calls = fakeFetch(() => json(201, placed));
    await placeOrder(api(), cart(), () => undefined);
    expect(body(calls[0])).toEqual({ restaurant_id: 5, lines: [{ item_id: 1, size: 'M', chosen: { '10': [101] }, qty: 2 }] });
  });

  test('a successful order clears the cart and returns the order', async () => {
    fakeFetch(() => json(201, placed));
    const clear = jest.fn();
    await expect(placeOrder(api(), cart(), clear)).resolves.toEqual(placed);
    expect(clear).toHaveBeenCalledTimes(1);
  });

  test('a network error keeps the cart so the retry can reuse the key', async () => {
    fakeFetch(() => { throw new TypeError('Network request failed'); });
    const clear = jest.fn();
    await expect(placeOrder(api(), cart(), clear)).rejects.toBeInstanceOf(TypeError);
    expect(clear).not.toHaveBeenCalled();
  });

  test('a retry after a network error sends the same key', async () => {
    const calls = fakeFetch(n => (n === 1 ? Promise.reject(new TypeError('Network request failed')) : json(200, placed)) as never);
    const snapshot = cart();
    await placeOrder(api(), snapshot, () => undefined).catch(() => undefined);
    await placeOrder(api(), snapshot, () => undefined);
    expect(calls.map(c => header(c, 'Idempotency-Key'))).toEqual(['key-1', 'key-1']);
  });

  test('a server rejection keeps the cart too', async () => {
    fakeFetch(() => json(400, { error: 'Crust: pick 1..1, got 2' }));
    const clear = jest.fn();
    await expect(placeOrder(api(), cart(), clear)).rejects.toBeInstanceOf(ApiError);
    expect(clear).not.toHaveBeenCalled();
  });

  test('an empty cart is refused without calling the API', async () => {
    const calls = fakeFetch(() => json(201, placed));
    await expect(placeOrder(api(), { restaurantId: null, lines: [], idempotencyKey: null }, () => undefined)).rejects.toThrow(/empty/i);
    expect(calls).toHaveLength(0);
  });

  test('error messages tell the user whether retrying is safe', () => {
    expect(checkoutErrorMessage(new TypeError('Network request failed'))).toMatch(/cart is saved/i);
    expect(checkoutErrorMessage(new ApiError(409, null))).toMatch(/still being placed/i);
    expect(checkoutErrorMessage(new ApiError(400, { error: 'restaurant is closed' }))).toBe('restaurant is closed');
    expect(checkoutErrorMessage(new ApiError(429, null, 4))).toMatch(/4 s/);
  });
});
