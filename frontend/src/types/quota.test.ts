import { describe, expect, it } from 'vitest';
import { formatQuotaAvailable, formatQuotaUsage } from './quota';

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

    it('preserves countable usage while exposing available balance separately', () => {
        const window = {
            ...baseWindow,
            unknown: false,
            used: 30,
            limit: 100,
            used_percent: 30,
            unit: 'credits',
            available: 70,
        };

        expect(formatQuotaUsage(window)).toBe('30 / 100 credits');
        expect(formatQuotaAvailable(window)).toBe('70 credits');
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
