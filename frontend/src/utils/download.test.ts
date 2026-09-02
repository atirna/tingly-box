import { describe, expect, it } from 'vitest';
import { extensionForMime, slugify } from './download';

describe('slugify', () => {
    it('builds a filename-safe stem', () => {
        expect(slugify('A sleepy orange cat!')).toBe('a-sleepy-orange-cat');
    });

    it('keeps CJK subjects instead of emptying the name', () => {
        expect(slugify('犯困的橘猫')).toBe('犯困的橘猫');
    });

    it('truncates without leaving a trailing separator', () => {
        expect(slugify('a very long prompt that runs past the limit', 12)).toBe('a-very-long');
    });

    it('falls back when nothing usable survives', () => {
        expect(slugify('!!! ???')).toBe('image');
    });
});

describe('extensionForMime', () => {
    it('names the file after what the provider actually returned', () => {
        expect(extensionForMime('image/jpeg')).toBe('jpg');
        expect(extensionForMime('image/webp')).toBe('webp');
        expect(extensionForMime('image/svg+xml')).toBe('svg');
    });

    it('falls back to png for an unknown or missing type', () => {
        expect(extensionForMime('application/octet-stream')).toBe('png');
        expect(extensionForMime('')).toBe('png');
    });
});
