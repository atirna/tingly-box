import { describe, expect, it } from 'vitest';
import { getUsageMetricColumns } from './usageMetricColumns';

describe('getUsageMetricColumns', () => {
    it('keeps the shared dashboard metric order', () => {
        expect(getUsageMetricColumns().map((column) => column.key)).toEqual([
            'requests',
            'cacheRead',
            'cacheHit',
            'input',
            'output',
            'reasoning',
            'errorRate',
        ]);
    });

    it('inserts optional total and cache-write columns in their semantic positions', () => {
        expect(getUsageMetricColumns({
            showTotal: true,
            showCacheWrite: true,
        }).map((column) => column.key)).toEqual([
            'requests',
            'total',
            'cacheRead',
            'cacheWrite',
            'cacheHit',
            'input',
            'output',
            'reasoning',
            'errorRate',
        ]);
    });
});
