import { useCallback, useState } from 'react';

// useCopyFeedback: copy text to the clipboard and flip a "copied" flag for a
// couple seconds so the caller can swap a tooltip/icon to acknowledge it.
export function useCopyFeedback(resetMs = 2000) {
    const [copied, setCopied] = useState(false);
    const copy = useCallback(
        (text: string) => {
            navigator.clipboard.writeText(text).then(() => {
                setCopied(true);
                setTimeout(() => setCopied(false), resetMs);
            });
        },
        [resetMs],
    );
    return { copied, copy };
}
