import { describe, it, expect } from 'vitest';
import { computeShare, OTHERS_SHARE_KEY } from './RosterTopList';

const item = (key: string, tokens: number) => ({ key, name: key, tokens });

describe('computeShare', () => {
    it('ranks by tokens, filters zero-usage, and computes shares against the full total', () => {
        const { rows, others, total } = computeShare([
            item('a', 100),
            item('b', 300),
            item('zero', 0),
            item('c', 100),
        ]);
        expect(rows.map((r) => r.key)).toEqual(['b', 'a', 'c']);
        expect(total).toBe(500);
        expect(rows[0].share).toBe(60);
        expect(others).toBeNull();
    });

    it('folds the tail beyond topN into a single others bucket', () => {
        const items = [10, 9, 8, 7, 6, 5, 4, 3].map((n, i) => item(`s${i}`, n));
        const { rows, others, total } = computeShare(items);
        expect(rows).toHaveLength(6);
        expect(total).toBe(52);
        expect(others?.key).toBe(OTHERS_SHARE_KEY);
        expect(others?.tokens).toBe(7); // 4 + 3
        expect(others?.share).toBeCloseTo((7 / 52) * 100);
    });

    it('keeps every subject as its own row when at or below topN', () => {
        const items = [5, 4, 3, 2, 1].map((n, i) => item(`s${i}`, n));
        const { rows, others } = computeShare(items);
        expect(rows).toHaveLength(5);
        expect(others).toBeNull();
    });

    it('returns empty rows when nothing in the period has usage', () => {
        const { rows, others, total } = computeShare([item('a', 0), item('b', 0)]);
        expect(rows).toEqual([]);
        expect(others).toBeNull();
        expect(total).toBe(0);
    });

    it('splits evenly on ties', () => {
        const { rows, total } = computeShare([item('a', 50), item('b', 50)]);
        expect(total).toBe(100);
        expect(rows[0].share).toBe(50);
        expect(rows[1].share).toBe(50);
    });
});
