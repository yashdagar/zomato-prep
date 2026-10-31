import { ApiClient } from '@app/api';
import { controlKind, dropUnavailable, errorsByGroup, fetchQuote, initialChosen, isAvailable, optionPrice, toggle } from '@app/configurator';
import type { MenuGroup, MenuItem } from '@app/menu';
import { body, fakeFetch, json, memStore } from './fakes';

const crust: MenuGroup = { id: 10, name: 'Crust', min_select: 1, max_select: 1, options: [
  { id: 101, name: 'Hand tossed', in_stock: true, prices: { '*': 0 } },
  { id: 102, name: 'Cheese burst', in_stock: true, prices: { M: 9900, L: 12900 } },
] };
const toppings: MenuGroup = { id: 20, name: 'Toppings', min_select: 0, max_select: 2, options: [
  { id: 201, name: 'Olives', in_stock: true, prices: { '*': 3000 } },
  { id: 202, name: 'Jalapeno', in_stock: true, prices: { '*': 3000 } },
  { id: 203, name: 'Paneer', in_stock: false, prices: { '*': 5000 } },
] };
const pizza: MenuItem = { id: 1, name: 'Farmhouse', sizes: [{ size: 'S', price_paise: 24900 }, { size: 'M', price_paise: 39900 }], groups: [crust, toppings] };

describe('W4 B5 configurator', () => {
  test('a group with max 1 is a radio group and others are checkboxes', () => {
    expect(controlKind(crust)).toBe('radio');
    expect(controlKind(toppings)).toBe('checkbox');
  });

  test('an option is available only when in stock and priced for the size or for every size', () => {
    expect(isAvailable(crust.options[1], 'M')).toBe(true);
    expect(isAvailable(crust.options[1], 'S')).toBe(false);
    expect(isAvailable(crust.options[0], 'S')).toBe(true);
    expect(isAvailable(toppings.options[2], 'M')).toBe(false);
  });

  test('the option price prefers the size price over the every-size price', () => {
    expect(optionPrice(crust.options[1], 'L')).toBe(12900);
    expect(optionPrice(toppings.options[0], 'S')).toBe(3000);
  });

  test('picking in a radio group replaces the previous pick', () => {
    expect(toggle(crust, { '10': [101] }, 102)).toEqual({ '10': [102] });
  });

  test('picking a chosen option again unpicks it', () => {
    expect(toggle(toppings, { '20': [201, 202] }, 201)).toEqual({ '20': [202] });
  });

  test('a checkbox group refuses picks beyond its max', () => {
    expect(toggle(toppings, { '20': [201, 202] }, 203)).toEqual({ '20': [201, 202] });
  });

  test('toggling leaves the input untouched', () => {
    const chosen = { '20': [201] };
    toggle(toppings, chosen, 202);
    expect(chosen).toEqual({ '20': [201] });
  });

  test('a required single-choice group starts on its first available option', () => {
    expect(initialChosen(pizza, 'S')).toEqual({ '10': [101] });
  });

  test('changing size drops picks that the new size does not offer', () => {
    expect(dropUnavailable(pizza, 'S', { '10': [102], '20': [201] })).toEqual({ '20': [201] });
  });

  test('server errors are matched to the group they belong to', () => {
    const { byGroup, general } = errorsByGroup(pizza, [
      'Crust: pick 1..1, got 2',
      'Cheese burst not available in S',
      'Toppings: duplicate option 201',
      'size XL not offered',
    ]);
    expect(byGroup['10']).toEqual(['Crust: pick 1..1, got 2', 'Cheese burst not available in S']);
    expect(byGroup['20']).toEqual(['Toppings: duplicate option 201']);
    expect(general).toEqual(['size XL not offered']);
  });

  test('the quote request matches the price endpoint body', async () => {
    const calls = fakeFetch(() => json(200, { description: 'Farmhouse (M)', unit_paise: 39900, total_paise: 39900, errors: null }));
    const quote = await fetchQuote(new ApiClient('http://api', memStore(null)), { itemId: 1, size: 'M', chosen: { '10': [101] }, qty: 1 });
    expect(calls[0].url).toBe('http://api/v1/menu/price');
    expect(calls[0].init.method).toBe('POST');
    expect(body(calls[0])).toEqual({ item_id: 1, size: 'M', chosen: { '10': [101] }, qty: 1 });
    expect(quote.unit_paise).toBe(39900);
  });
});
