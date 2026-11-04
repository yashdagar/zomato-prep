import { ApiClient, ApiError } from '@app/api';
import { isSoldOutError, payOrder, payScreenState, stockLabel } from '@app/stretch';
import { body, fakeFetch, header, json, memStore } from './fakes';

const api = () => new ApiClient('http://api', memStore({ access: 'A', refresh: 'R' }));

describe('W8 B3 stretch screens', () => {
  test('an item with no stock figure shows no label and can be added', () => {
    expect(stockLabel(null)).toEqual({ text: null, disabled: false });
    expect(stockLabel(undefined)).toEqual({ text: null, disabled: false });
  });

  test('plenty of stock shows no label', () => {
    expect(stockLabel(6)).toEqual({ text: null, disabled: false });
  });

  test('five or fewer left shows how many remain', () => {
    expect(stockLabel(5)).toEqual({ text: 'Only 5 left', disabled: false });
    expect(stockLabel(1)).toEqual({ text: 'Only 1 left', disabled: false });
  });

  test('zero stock shows Sold out and disables adding', () => {
    expect(stockLabel(0)).toEqual({ text: 'Sold out', disabled: true });
  });

  test('a 409 with the sold_out code is recognised as sold out', () => {
    expect(isSoldOutError(new ApiError(409, { code: 'sold_out' }))).toBe(true);
  });

  test('other errors are not treated as sold out', () => {
    expect(isSoldOutError(new ApiError(409, { code: 'in_flight' }))).toBe(false);
    expect(isSoldOutError(new ApiError(500, { code: 'sold_out' }))).toBe(false);
    expect(isSoldOutError(new TypeError('Network request failed'))).toBe(false);
    expect(isSoldOutError(null)).toBe(false);
  });

  test('paying posts the chosen outcome to the order payment route', async () => {
    const calls = fakeFetch(() => json(200, { status: 'PLACED' }));
    await payOrder(api(), 42, 'success');
    expect(calls[0].url).toBe('http://api/v1/payments/42/simulate');
    expect(calls[0].init.method).toBe('POST');
    expect(body(calls[0])).toEqual({ outcome: 'success' });
    expect(header(calls[0], 'Authorization')).toBe('Bearer A');
  });

  test('paying returns the order status the server reports', async () => {
    fakeFetch(() => json(200, { status: 'CANCELLED' }));
    await expect(payOrder(api(), 42, 'fail')).resolves.toEqual({ status: 'CANCELLED' });
  });

  test('a failed payment call rejects with the ApiError', async () => {
    fakeFetch(() => json(404, { error: 'unknown order' }));
    await expect(payOrder(api(), 42, 'success')).rejects.toBeInstanceOf(ApiError);
  });

  test('an order waiting for payment shows the Pay button', () => {
    expect(payScreenState('PENDING_PAYMENT')).toEqual({ showPay: true, title: 'Pay now' });
  });

  test('a placed order shows payment received and no button', () => {
    expect(payScreenState('PLACED')).toEqual({ showPay: false, title: 'Payment received' });
  });

  test('a cancelled order explains the failed payment', () => {
    expect(payScreenState('CANCELLED')).toEqual({ showPay: false, title: 'Payment failed, order cancelled' });
  });

  test('any later status falls back to a neutral title', () => {
    expect(payScreenState('PREPARING')).toEqual({ showPay: false, title: 'Order in progress' });
  });
});
