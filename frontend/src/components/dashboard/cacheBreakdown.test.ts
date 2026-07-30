import { describe, it, expect } from 'vitest';
import { formatCacheBreakdown, getCacheHitRate } from './chartStyles';

const fmt = (n: number) => n.toLocaleString();

describe('formatCacheBreakdown', () => {
    it('omits the write half when the channel reports no writes', () => {
        // Pre-gpt-5.6 models and Azure never surface cache writes, so a "0 written"
        // suffix would be permanent noise rather than information.
        expect(formatCacheBreakdown(1200, 0, fmt)).toBe('1,200 read');
    });

    it('shows reads and writes side by side once writes exist', () => {
        expect(formatCacheBreakdown(1200, 340, fmt)).toBe('1,200 read · 340 written');
    });

    it('still reports writes when nothing was read from cache', () => {
        // First request against a fresh prefix: everything is written, nothing hit.
        expect(formatCacheBreakdown(0, 512, fmt)).toBe('0 read · 512 written');
    });
});

describe('getCacheHitRate', () => {
    it('excludes cache writes from the denominator by construction', () => {
        // inputTokens already contains the write portion — the hit rate is
        // read / (read + input), so writes correctly count as a miss, not a hit.
        expect(getCacheHitRate(800, 200)).toBeCloseTo(80);
    });

    it('returns 0 rather than NaN with no traffic', () => {
        expect(getCacheHitRate(0, 0)).toBe(0);
    });
});
