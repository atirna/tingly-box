import {
    Box,
    Divider,
    MenuItem,
    Select,
    Stack,
    Tooltip,
    Typography,
} from '@mui/material';
import { InfoOutlined as InfoOutlinedIcon } from '@/components/icons';
import React from 'react';
import { useTranslation } from 'react-i18next';

// DshPrefs mirrors the Go struct in internal/server/config (DshPrefs). Keys
// are the literal settings.yaml provider-stanza keys so the object
// round-trips through the backend without an intermediate mapping layer.
// All values are strings; "" means "omit this key, dsh treats the provider
// as text-only".
export interface DshPrefs {
    default_input?: string; // "text" | "text_image" | ""
}

export function defaultDshPrefs(): DshPrefs {
    // Conservative default: no defaultInput key, so dsh treats the provider
    // as text-only until the user opts a model into vision — keep this in
    // sync with Go's DefaultDshPrefs.
    return {};
}

// DSH_PREF_KEYS is the single source of truth for the durable dsh pref keys
// on the frontend (mirrors CODEX_PREF_KEYS in CodexQuickConfig.tsx and the
// backend DshPrefs struct).
const DSH_PREF_KEYS = ['default_input'] as const satisfies readonly (keyof DshPrefs)[];

// Merge a previously-applied prefs object over the current defaults so
// reopening the dsh config modal restores durable user choices rather than
// resetting to defaults every time.
export function mergeSavedDshPrefs(applied: DshPrefs = {}): DshPrefs {
    const merged: DshPrefs = { ...defaultDshPrefs() };
    for (const key of DSH_PREF_KEYS) {
        const v = applied[key];
        if (v !== undefined && v !== '') {
            merged[key] = v;
        }
    }
    return merged;
}

type Lang = 'zh' | 'en';

const UNSET = '';
const DEFAULT_INPUT_VALUES = ['text', 'text_image'];

interface FieldText {
    label: string;
    purpose: string;
    tooltip: string;
}

const FIELD_TEXT: Record<Lang, FieldText> = {
    zh: {
        label: '支持的输入模态',
        purpose: '控制该 provider 下的模型默认能否接收图片',
        tooltip: 'text 仅文本；text_image 允许图片输入。留空表示不写入 defaultInput，dsh 按文本模型处理。',
    },
    en: {
        label: 'Supported input modality',
        purpose: 'Whether models under this provider accept image input by default',
        tooltip: 'text is text-only; text_image also accepts images. Empty = omit defaultInput, dsh treats models as text-only.',
    },
};

const VALUE_LABEL: Record<Lang, Record<string, string>> = {
    zh: { text: 'text（仅文本）', text_image: 'text_image（文本 + 图片）' },
    en: { text: 'text (text-only)', text_image: 'text_image (text + image)' },
};

const UI_TEXT: Record<Lang, { panelHeader: string; sectionTitle: string; sectionHint: string; unsetLabel: string }> = {
    zh: {
        panelHeader: '这些项写入 $DSH_HOME/settings.yaml 的 tingly-box provider 条目',
        sectionTitle: '模型能力',
        sectionHint: '留空表示用 dsh 默认（仅文本）',
        unsetLabel: '（默认，仅文本）',
    },
    en: {
        panelHeader: 'Written into the tingly-box provider entry in $DSH_HOME/settings.yaml',
        sectionTitle: 'Model capabilities',
        sectionHint: 'Empty = dsh default (text-only)',
        unsetLabel: '(default, text-only)',
    },
};

function useLang(): Lang {
    const { i18n } = useTranslation();
    return i18n.language === 'zh' ? 'zh' : 'en';
}

interface DshQuickConfigProps {
    prefs: DshPrefs;
    setPrefs: (p: DshPrefs) => void;
}

const DshQuickConfig: React.FC<DshQuickConfigProps> = ({ prefs, setPrefs }) => {
    const lang = useLang();
    const uiText = UI_TEXT[lang];
    const text = FIELD_TEXT[lang];
    const valueLabel = VALUE_LABEL[lang];

    const value = prefs.default_input ?? '';
    const setValue = (next: string) => setPrefs({ ...prefs, default_input: next });

    const richTooltip = (
        <Box sx={{ maxWidth: 280 }}>
            <Typography variant="caption" sx={{ display: 'block', mb: 0.5 }}>{text.purpose}</Typography>
            <Typography variant="caption" sx={{ display: 'block', opacity: 0.85 }}>{text.tooltip}</Typography>
        </Box>
    );

    return (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
            <Typography variant="body2" sx={{ color: 'text.secondary' }}>{uiText.panelHeader}</Typography>
            <Box>
                <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1.5, mb: 0.5 }}>
                    <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>{uiText.sectionTitle}</Typography>
                    <Typography variant="caption" sx={{ color: 'text.secondary' }}>{uiText.sectionHint}</Typography>
                </Box>
                <Divider />
                <Stack>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, py: 1, minHeight: 44 }}>
                        <Box sx={{ flex: '0 0 180px', display: 'flex', alignItems: 'center', gap: 0.5, minWidth: 0 }}>
                            <Typography variant="body2" noWrap sx={{ fontWeight: 500 }}>{text.label}</Typography>
                            <Tooltip placement="top" arrow title={richTooltip}>
                                <InfoOutlinedIcon sx={{ fontSize: 14, color: 'text.disabled', cursor: 'help' }} />
                            </Tooltip>
                        </Box>
                        <Box sx={{ flex: '0 0 320px', minWidth: 0 }}>
                            <Box
                                component="span"
                                sx={{
                                    px: 0.75,
                                    py: 0.25,
                                    borderRadius: 0.75,
                                    bgcolor: 'action.hover',
                                    fontFamily: 'monospace',
                                    fontSize: '0.72rem',
                                    color: 'text.secondary',
                                    whiteSpace: 'nowrap',
                                }}
                            >
                                defaultInput
                            </Box>
                        </Box>
                        <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'flex-end' }}>
                            <Select
                                size="small"
                                value={value}
                                displayEmpty
                                onChange={(e) => setValue(e.target.value)}
                                sx={{ minWidth: 220, fontSize: '0.85rem' }}
                            >
                                <MenuItem value={UNSET}>
                                    <Typography variant="body2" sx={{ color: 'text.disabled' }}>{uiText.unsetLabel}</Typography>
                                </MenuItem>
                                {DEFAULT_INPUT_VALUES.map((v) => (
                                    <MenuItem key={v} value={v} sx={{ fontSize: '0.85rem' }}>{valueLabel[v]}</MenuItem>
                                ))}
                            </Select>
                        </Box>
                    </Box>
                </Stack>
            </Box>
        </Box>
    );
};

export default DshQuickConfig;
