import { describe, expect, it } from 'vitest';
import { formatQuotaUsage } from './quota';

const baseWindow = {
    used: 0,
    limit: 0,
    used_percent: 0,
    unit: 'currency',
    unknown: true,
};

describe('formatQuotaUsage', () => {
    it('shows a reported available balance even when its percentage is unknown', () => {
        expect(formatQuotaUsage({
            ...baseWindow,
            available: 81.41,
            currency_code: 'CNY',
        })).toBe('81.41 CNY');
    });

    it('keeps truly unknown values distinct from a zero balance', () => {
        expect(formatQuotaUsage(baseWindow)).toBe('not reported');
        expect(formatQuotaUsage({
            ...baseWindow,
            available: 0,
            currency_code: 'CNY',
        })).toBe('0 CNY');
    });
});
