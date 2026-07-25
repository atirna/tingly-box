import {Box, useTheme, alpha} from '@mui/material';
import type {SxProps, Theme} from '@mui/material';
import { EMPTY_SX } from '@/constants/defaults';

// Parse a #RRGGBB (or #RGB) hex string into an {r,g,b} triple. Returns null for
// anything else (css color names, rgb() strings) so callers can fall back safely.
const hexToRgb = (hex: string): { r: number; g: number; b: number } | null => {
    let h = hex.trim();
    if (!h.startsWith('#')) return null;
    h = h.slice(1);
    if (h.length === 3) h = h.split('').map((c) => c + c).join('');
    if (h.length !== 6) return null;
    const num = parseInt(h, 16);
    if (Number.isNaN(num)) return null;
    return { r: (num >> 16) & 255, g: (num >> 8) & 255, b: num & 255 };
};

interface ApiStyleBadgeProps {
    apiStyle: string;
    compact?: boolean;
    minimal?: boolean;
    sx?: SxProps<Theme>;
}

// Helper function to render API style badge with icon and colored background
export const ApiStyleBadge = ({apiStyle, sx = EMPTY_SX, compact = false, minimal = false}: ApiStyleBadgeProps) => {
    const theme = useTheme();
    const isOpenAI = apiStyle === 'openai';
    const isAnthropic = apiStyle === 'anthropic';
    const isGoogle = apiStyle === 'google';

    if (!isOpenAI && !isAnthropic && !isGoogle) {
        return null; // Don't show badge for unknown styles
    }

    // The badge floats over the service node's own text (provider name), so its
    // background must be FULLY OPAQUE — any alpha lets that text bleed through.
    // We blend the brand tint onto the theme's paper color directly, producing a
    // solid color per mode rather than a transparent wash. Mirrors ActionButtonsBox,
    // which layers a solid paper backing for the same reason (see nodes/styles.tsx).
    const paper = theme.palette.background.paper;
    const isDark = theme.palette.mode === 'dark';

    // Composite a translucent tint over an opaque paper base → one opaque color.
    const blend = (tint: string, tintAlpha: number) => {
        const t = hexToRgb(tint);
        const p = hexToRgb(paper);
        if (!t || !p) return paper;
        const a = tintAlpha;
        const r = Math.round(t.r * a + p.r * (1 - a));
        const g = Math.round(t.g * a + p.g * (1 - a));
        const b = Math.round(t.b * a + p.b * (1 - a));
        return `rgb(${r}, ${g}, ${b})`;
    };

    const tints = {
        openai: { tint: theme.palette.info.main, fill: isDark ? 0.22 : 0.14, border: alpha(theme.palette.info.main, 0.4) },
        anthropic: { tint: '#E07A5F', fill: isDark ? 0.26 : 0.16, border: alpha('#E07A5F', 0.5) },
        google: { tint: '#4285F4', fill: isDark ? 0.22 : 0.14, border: alpha('#4285F4', 0.4) },
        fallback: { tint: theme.palette.grey[500], fill: isDark ? 0.22 : 0.14, border: alpha(theme.palette.grey[500], 0.4) },
    } as const;
    const key = isOpenAI ? 'openai' : isAnthropic ? 'anthropic' : isGoogle ? 'google' : 'fallback';
    const t = tints[key];

    const getBadgeStyles = () => ({
        backgroundColor: blend(t.tint, t.fill), // opaque — no bleed-through
        color: t.tint,
        borderColor: t.border,
    });

    const label = isOpenAI ? 'OpenAI' : isAnthropic ? 'Anthropic' : 'Google';
    const letter = isOpenAI ? 'O' : isAnthropic ? 'A' : 'G';
    const badgeStyles = getBadgeStyles();

    if (minimal) {
        return (
            <Box
                sx={{
                    width: 16,
                    height: 16,
                    borderRadius: '50%',
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    flexShrink: 0,
                    fontSize: '9px',
                    fontWeight: 700,
                    lineHeight: 1,
                    border: `1px solid ${badgeStyles.borderColor}`,
                    backgroundColor: badgeStyles.backgroundColor,
                    color: badgeStyles.color,
                    ...sx,
                }}
            >
                {letter}
            </Box>
        );
    }

    return (
        <Box
            sx={{
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: 0.5,
                px: compact ? 0.65 : 1.1,
                py: compact ? 0.125 : 0.375,
                borderRadius: theme.shape.borderRadius,
                fontSize: compact ? '8px' : '10px',
                fontWeight: 600,
                height: compact ? '16px' : '20px',
                minWidth: compact ? 'unset' : '76px',
                border: `1px solid ${badgeStyles.borderColor}`,
                backgroundColor: badgeStyles.backgroundColor,
                color: badgeStyles.color,
                transition: theme.transitions.create(['background-color', 'color', 'border-color'], {
                    duration: theme.transitions.duration.shorter,
                }),
                '&:hover': {
                    backgroundColor: blend(t.tint, isDark ? 0.32 : 0.22),
                    borderColor: alpha(t.tint, 0.6),
                },
                ...sx,
            }}
        >
            {compact ? (<span>{label}</span>) : (<span>{label} Style</span>)}
        </Box>
    );
};