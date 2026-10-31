import { ApiClient } from '@app/api';
import { DEFAULT_SECTION, fetchMenu, formatPaise, lowestPrice, toSections, type MenuItem } from '@app/menu';
import { fakeFetch, json, memStore } from './fakes';

const item = (over: Partial<MenuItem>): MenuItem => ({ id: 1, name: 'Farmhouse', sizes: [{ size: 'M', price_paise: 39900 }], groups: [], ...over });

describe('W4 B4 menu sections', () => {
  test('the lowest size price is the from price', () => {
    expect(lowestPrice(item({ sizes: [{ size: 'L', price_paise: 59900 }, { size: 'S', price_paise: 24900 }, { size: 'M', price_paise: 39900 }] }))).toBe(24900);
  });

  test('items group into sections by category in menu order', () => {
    const sections = toSections({ items: [
      item({ id: 1, category: 'Pizzas' }),
      item({ id: 2, category: 'Sides' }),
      item({ id: 3, category: 'Pizzas' }),
    ] });
    expect(sections.map(s => s.title)).toEqual(['Pizzas', 'Sides']);
    expect(sections[0].data.map(r => r.id)).toEqual([1, 3]);
  });

  test('items without a category land in one default section', () => {
    const sections = toSections({ items: [item({ id: 1 }), item({ id: 2, category: '  ' })] });
    expect(sections).toHaveLength(1);
    expect(sections[0].title).toBe(DEFAULT_SECTION);
  });

  test('each row carries its from price', () => {
    const [section] = toSections({ items: [item({ sizes: [{ size: 'L', price_paise: 50000 }, { size: 'S', price_paise: 20000 }] })] });
    expect(section.data[0].fromPaise).toBe(20000);
  });

  test('items with no sizes cannot be ordered and are left out', () => {
    expect(toSections({ items: [item({ id: 1, sizes: [] }), item({ id: 2 })] })[0].data.map(r => r.id)).toEqual([2]);
  });

  test('an empty menu has no sections', () => {
    expect(toSections({ items: [] })).toEqual([]);
  });

  test('paise format as rupees', () => {
    expect(formatPaise(39900)).toBe('₹399');
    expect(formatPaise(39950)).toBe('₹399.50');
  });

  test('the menu is fetched for the restaurant id', async () => {
    const calls = fakeFetch(() => json(200, { items: [] }));
    await fetchMenu(new ApiClient('http://api', memStore(null)), 42);
    expect(calls[0].url).toBe('http://api/v1/restaurants/42/menu');
  });
});
