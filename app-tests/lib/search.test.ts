import { canSearch, createDebouncer, normalizeTerm, searchKey, type Timers } from '@app/search';

function fakeTimers() {
  let now = 0;
  let nextId = 1;
  const pending = new Map<number, { at: number; fn: () => void }>();
  const timers: Timers = {
    setTimeout(fn, ms) {
      const id = nextId++;
      pending.set(id, { at: now + ms, fn });
      return id;
    },
    clearTimeout(id) {
      pending.delete(id as number);
    },
  };
  return {
    timers,
    pending: () => pending.size,
    advance(ms: number) {
      const target = now + ms;
      for (;;) {
        const due = [...pending.entries()].filter(([, t]) => t.at <= target).sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        pending.delete(due[0]);
        now = due[1].at;
        due[1].fn();
      }
      now = target;
    },
  };
}

describe('W4 O3 Debounced search', () => {
  test('nothing is called before the delay has passed', () => {
    const t = fakeTimers();
    const seen: string[] = [];
    const d = createDebouncer((v: string) => seen.push(v), 300, t.timers);
    d.call('piz');
    t.advance(299);
    expect(seen).toEqual([]);
  });

  test('the callback runs once with the latest value after the quiet period', () => {
    const t = fakeTimers();
    const seen: string[] = [];
    const d = createDebouncer((v: string) => seen.push(v), 300, t.timers);
    d.call('p');
    t.advance(100);
    d.call('pi');
    t.advance(100);
    d.call('piz');
    t.advance(300);
    expect(seen).toEqual(['piz']);
  });

  test('every new call restarts the wait', () => {
    const t = fakeTimers();
    const seen: string[] = [];
    const d = createDebouncer((v: string) => seen.push(v), 300, t.timers);
    d.call('a');
    t.advance(250);
    d.call('ab');
    t.advance(250);
    expect(seen).toEqual([]);
    t.advance(50);
    expect(seen).toEqual(['ab']);
  });

  test('separate bursts each produce their own call', () => {
    const t = fakeTimers();
    const seen: string[] = [];
    const d = createDebouncer((v: string) => seen.push(v), 100, t.timers);
    d.call('one');
    t.advance(100);
    d.call('two');
    t.advance(100);
    expect(seen).toEqual(['one', 'two']);
  });

  test('cancel drops the pending call and clears its timer', () => {
    const t = fakeTimers();
    const seen: string[] = [];
    const d = createDebouncer((v: string) => seen.push(v), 100, t.timers);
    d.call('x');
    d.cancel();
    t.advance(500);
    expect(seen).toEqual([]);
    expect(t.pending()).toBe(0);
  });

  test('flush runs the pending call immediately and only once', () => {
    const t = fakeTimers();
    const seen: string[] = [];
    const d = createDebouncer((v: string) => seen.push(v), 100, t.timers);
    d.call('now');
    d.flush();
    expect(seen).toEqual(['now']);
    t.advance(500);
    expect(seen).toEqual(['now']);
  });

  test('flush with nothing pending does nothing', () => {
    const t = fakeTimers();
    const seen: string[] = [];
    const d = createDebouncer((v: string) => seen.push(v), 100, t.timers);
    d.flush();
    expect(seen).toEqual([]);
  });

  test('normalizeTerm trims, lowercases and collapses inner whitespace', () => {
    expect(normalizeTerm('  Butter   CHICKEN ')).toBe('butter chicken');
  });

  test('terms that differ only in case or spacing share one query key', () => {
    expect(searchKey('  Pizza ')).toEqual(searchKey('pizza'));
  });

  test('different terms get different query keys', () => {
    expect(searchKey('pizza')).not.toEqual(searchKey('pasta'));
  });

  test('the key starts with restaurants so invalidating that prefix also clears searches', () => {
    expect(searchKey('pizza').slice(0, 2)).toEqual(['restaurants', 'search']);
    expect(searchKey('pizza')[2]).toBe('pizza');
  });

  test('canSearch needs at least two characters after normalizing', () => {
    expect(canSearch(' a ')).toBe(false);
    expect(canSearch('')).toBe(false);
    expect(canSearch('pi')).toBe(true);
  });
});
