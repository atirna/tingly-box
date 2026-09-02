// Saving a blob to the user's disk, and the naming that goes with it.
//
// Deliberately not part of any one feature: the anchor-click dance had already
// been hand-rolled twice in this codebase before this module existed, and each
// copy learned (or failed to learn) the revoke timing separately.

export const downloadBlob = (blob: Blob, fileName: string): void => {
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = fileName;
    document.body.appendChild(link);
    link.click();
    link.remove();
    // Revoking synchronously can cancel the download in some browsers.
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
};

export const downloadText = (content: string, fileName: string, mimeType: string): void =>
    downloadBlob(new Blob([content], { type: mimeType }), fileName);

/** Filename-safe stem derived from free text, so downloads are recognisable. */
export const slugify = (text: string, maxLength = 32): string => {
    const slug = text
        .toLowerCase()
        .replace(/[^a-z0-9一-龥]+/g, '-')
        .replace(/^-+|-+$/g, '')
        .slice(0, maxLength)
        .replace(/-+$/g, '');
    return slug || 'image';
};

const MIME_EXTENSIONS: Record<string, string> = {
    'image/png': 'png',
    'image/jpeg': 'jpg',
    'image/webp': 'webp',
    'image/svg+xml': 'svg',
};

/** Extension for a blob's own type — providers do not all hand back PNG. */
export const extensionForMime = (mimeType: string): string => MIME_EXTENSIONS[mimeType] ?? 'png';

/**
 * Reads any image source — a data URL or a provider's remote URL — as a blob.
 * Going through fetch is what lets a remote image be drawn into a canvas
 * later: an <img> pointed straight at a cross-origin URL taints it.
 */
export const fetchBlob = async (src: string): Promise<Blob> => {
    const response = await fetch(src);
    if (!response.ok) throw new Error(`fetch failed: ${response.status}`);
    return response.blob();
};

/** Fetches an image and saves it under `<stem>.<its own extension>`. */
export const downloadImage = async (src: string, stem: string): Promise<void> => {
    const blob = await fetchBlob(src);
    downloadBlob(blob, `${stem}.${extensionForMime(blob.type)}`);
};
