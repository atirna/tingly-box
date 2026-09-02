import { describe, expect, it } from 'vitest';
import { computeTileRects, tileFileName } from './imageSlice';

describe('computeTileRects', () => {
    it('divides an image into rows x cols tiles covering the whole frame', () => {
        const rects = computeTileRects(900, 900, { rows: 3, cols: 3, margin: 0, gutter: 0 });
        expect(rects).toHaveLength(9);
        expect(rects[0]).toMatchObject({ index: 0, row: 0, col: 0, x: 0, y: 0, width: 300, height: 300 });
        expect(rects[8]).toMatchObject({ index: 8, row: 2, col: 2, x: 600, y: 600, width: 300, height: 300 });
    });

    it('trims the outer margin against the shorter side so the trim stays isotropic', () => {
        const rects = computeTileRects(1000, 500, { rows: 1, cols: 1, margin: 0.1, gutter: 0 });
        expect(rects[0]).toMatchObject({ x: 50, y: 50, width: 900, height: 400 });
    });

    it('removes the gutter symmetrically, keeping every tile the same size', () => {
        const rects = computeTileRects(400, 400, { rows: 2, cols: 2, margin: 0, gutter: 0.2 });
        const widths = new Set(rects.map((rect) => rect.width));
        const heights = new Set(rects.map((rect) => rect.height));
        expect(widths).toEqual(new Set([160]));
        expect(heights).toEqual(new Set([160]));
        expect(rects[0]).toMatchObject({ x: 20, y: 20 });
        expect(rects[3]).toMatchObject({ x: 220, y: 220 });
    });

    it('never returns tiles outside the image or of zero size', () => {
        const rects = computeTileRects(120, 80, { rows: 4, cols: 4, margin: 0.9, gutter: 0.99 });
        for (const rect of rects) {
            expect(rect.width).toBeGreaterThan(0);
            expect(rect.height).toBeGreaterThan(0);
            expect(rect.x + rect.width).toBeLessThanOrEqual(120);
            expect(rect.y + rect.height).toBeLessThanOrEqual(80);
        }
    });

    it('clamps a degenerate grid to at least one tile', () => {
        expect(computeTileRects(100, 100, { rows: 0, cols: 0, margin: 0, gutter: 0 })).toHaveLength(1);
    });
});

describe('tileFileName', () => {
    it('pads the index to the width of the total', () => {
        expect(tileFileName('cat', 0, 9)).toBe('cat-1.png');
        expect(tileFileName('cat', 9, 16)).toBe('cat-10.png');
        expect(tileFileName('cat', 0, 16)).toBe('cat-01.png');
    });
});
