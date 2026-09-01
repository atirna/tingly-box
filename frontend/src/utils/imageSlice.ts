// Client-side sticker-sheet slicing: cut one generated grid image into its
// individual tiles, entirely in the browser (canvas), so the user gets usable
// assets without a round trip or a second tool.
//
// The cut is a plain even grid — deliberately. Models do not place a sheet's
// cells on an exact lattice, so instead of guessing at content boundaries the
// user gets two honest knobs (outer margin, gutter) and a live overlay showing
// where the cuts land.

import { fetchBlob } from './download';

export interface GridSpec {
    rows: number;
    cols: number;
    /** Fraction of the image's shorter side trimmed off each outer edge. */
    margin: number;
    /** Fraction of a cell removed as spacing between neighbouring cells. */
    gutter: number;
}

export interface TileRect {
    index: number;
    row: number;
    col: number;
    x: number;
    y: number;
    width: number;
    height: number;
}

// 3x3 is the shape a grid image almost always comes back as. The two maxima
// are the single source of truth for both the slider bounds and the clamp
// below, so the UI can never offer a value the geometry would quietly reject.
export const DEFAULT_GRID: GridSpec = { rows: 3, cols: 3, margin: 0, gutter: 0 };
export const MARGIN_MAX = 0.2;
export const GUTTER_MAX = 0.4;

const clamp = (value: number, min: number, max: number): number => Math.min(Math.max(value, min), max);

/**
 * Cut rectangles for an evenly divided grid, in source-image pixels.
 *
 * Margin is measured against the shorter side so a trim stays visually
 * isotropic on non-square sheets; the gutter is taken out of each cell
 * symmetrically (half per side), which keeps every tile the same size.
 */
export const computeTileRects = (width: number, height: number, spec: GridSpec): TileRect[] => {
    const rows = Math.max(1, Math.floor(spec.rows));
    const cols = Math.max(1, Math.floor(spec.cols));
    const base = Math.min(width, height);
    const margin = clamp(spec.margin, 0, MARGIN_MAX) * base;
    const innerWidth = Math.max(1, width - margin * 2);
    const innerHeight = Math.max(1, height - margin * 2);
    const cellWidth = innerWidth / cols;
    const cellHeight = innerHeight / rows;
    const gutter = clamp(spec.gutter, 0, GUTTER_MAX);
    const insetX = (cellWidth * gutter) / 2;
    const insetY = (cellHeight * gutter) / 2;

    const rects: TileRect[] = [];
    for (let row = 0; row < rows; row += 1) {
        for (let col = 0; col < cols; col += 1) {
            const x = Math.round(margin + col * cellWidth + insetX);
            const y = Math.round(margin + row * cellHeight + insetY);
            const right = Math.round(margin + (col + 1) * cellWidth - insetX);
            const bottom = Math.round(margin + (row + 1) * cellHeight - insetY);
            rects.push({
                index: row * cols + col,
                row,
                col,
                x: clamp(x, 0, width),
                y: clamp(y, 0, height),
                width: Math.max(1, clamp(right, 0, width) - clamp(x, 0, width)),
                height: Math.max(1, clamp(bottom, 0, height) - clamp(y, 0, height)),
            });
        }
    }
    return rects;
};

/**
 * Loads any playground image source (data URL or provider URL) as an
 * <img>. Provider URLs are fetched first and handed to the image as an object
 * URL: drawing a cross-origin URL directly taints the canvas and makes
 * toBlob() throw, which is exactly the step this module exists to perform.
 */
export const loadImage = async (src: string): Promise<{ image: HTMLImageElement; release: () => void }> => {
    const objectUrl = URL.createObjectURL(await fetchBlob(src));
    try {
        const image = await new Promise<HTMLImageElement>((resolve, reject) => {
            const element = new Image();
            element.onload = () => resolve(element);
            element.onerror = () => reject(new Error('failed to decode image'));
            element.src = objectUrl;
        });
        return { image, release: () => URL.revokeObjectURL(objectUrl) };
    } catch (error) {
        URL.revokeObjectURL(objectUrl);
        throw error;
    }
};

const canvasToBlob = (canvas: HTMLCanvasElement): Promise<Blob> => new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
        if (blob) resolve(blob);
        else reject(new Error('failed to encode tile'));
    }, 'image/png');
});

/**
 * Renders one tile as a PNG. `exportSize`, when set, scales the tile so its
 * longer side matches that many pixels (sticker platforms tend to want 512).
 */
export const renderTile = async (
    image: HTMLImageElement,
    rect: TileRect,
    exportSize?: number | null,
): Promise<Blob> => {
    const scale = exportSize ? exportSize / Math.max(rect.width, rect.height) : 1;
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(rect.width * scale));
    canvas.height = Math.max(1, Math.round(rect.height * scale));
    const context = canvas.getContext('2d');
    if (!context) throw new Error('canvas 2d context unavailable');
    context.imageSmoothingQuality = 'high';
    context.drawImage(image, rect.x, rect.y, rect.width, rect.height, 0, 0, canvas.width, canvas.height);
    return canvasToBlob(canvas);
};

export const tileFileName = (stem: string, index: number, total: number): string => {
    const width = String(total).length;
    return `${stem}-${String(index + 1).padStart(width, '0')}.png`;
};
