import React, { memo, useState } from 'react';
import { Box, Typography, Tooltip, ToggleButton, ToggleButtonGroup, TextField, Stack, Collapse, Slider } from '@mui/material';
import { ExpandMore as ExpandMoreIcon, ExpandLess as ExpandLessIcon } from '@/components/icons';
import { useTranslation } from 'react-i18next';
import { toggleButtonGroupStyle } from '@/styles/toggleStyles';
import type { ProbeThinking, ProbeProtocol } from '@/types/probe';
import type { ProbeAxes } from './probeConfig';

// ProbeControls renders the control rail: orthogonal axes stacked vertically,
// one label + control pair per row. Groups fill the rail width and every
// option button is equal-width, so the rail reads as an aligned instrument
// panel instead of ragged inline chips. Adding a future axis = one more row
// here (and a field on ProbeAxes).

interface ProbeControlsProps {
    axes: ProbeAxes;
    onAxesChange: (axes: ProbeAxes) => void;
    message: string;
    onMessageChange: (message: string) => void;
    messagePlaceholder: string;
    // Protocol axis rendering, pre-resolved by the dialog (per-target
    // reduction: locked single option, or disabled for Google). '' while the
    // provider record is still loading.
    protocol: {
        value: ProbeProtocol | '';
        options: ProbeProtocol[];
        locked: boolean;
        disabled: boolean;
        lockHint?: string;
    };
    scopeDisabled: boolean;
    scopeHint: string;
}

// Rail-short labels; the full name rides in the hover tooltip.
export const PROTOCOL_LABELS: Partial<Record<ProbeProtocol | '', string>> = {
    openai_chat: 'O Chat',
    openai_responses: 'O Resp.',
    anthropic_v1: 'A',
};

const PROTOCOL_FULL: Partial<Record<ProbeProtocol | '', string>> = {
    openai_chat: 'OpenAI Chat Completions',
    openai_responses: 'OpenAI Responses API',
    anthropic_v1: 'Anthropic Messages',
};

// THINKING_LADDER orders the effort steps for the slider control bar.
const THINKING_LADDER: ProbeThinking[] = ['none', 'low', 'medium', 'high'];

// Full-width group with equal-width options — the alignment primitive of the
// rail. Minimal deltas over the shared theme style (width + flex); all
// visual styling (padding, colors, selected state, shape) stays themed.
const railGroupStyle = {
    ...toggleButtonGroupStyle,
    width: '100%',
    '& .MuiToggleButton-root': {
        ...((toggleButtonGroupStyle as Record<string, any>)['& .MuiToggleButton-root'] ?? {}),
        flex: 1,
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
    },
};

const Axis = memo(({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) => {
    const head = (
        <Typography
            variant="caption"
            sx={{
                color: 'text.secondary',
                cursor: hint ? 'help' : 'default',
                fontWeight: 500,
            }}
        >
            {label}
        </Typography>
    );
    return (
        <Box sx={{ minWidth: 0 }}>
            {hint ? <Tooltip title={hint}>{head}</Tooltip> : head}
            <Box sx={{ mt: 0.5 }}>{children}</Box>
        </Box>
    );
});

export const ProbeControls: React.FC<ProbeControlsProps> = ({
    axes,
    onAxesChange,
    message,
    onMessageChange,
    messagePlaceholder,
    protocol,
    scopeDisabled,
    scopeHint,
}) => {
    const { t } = useTranslation();
    const [advancedOpen, setAdvancedOpen] = useState(false);

    const set = (patch: Partial<ProbeAxes>) => onAxesChange({ ...axes, ...patch });

    return (
        <Stack spacing={1.5}>
            {/* Primary axes: what 80% of probes touch. */}
            <Axis label={t('probe.shape')} hint={t('probe.shapeHint')}>
                <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={axes.stream ? 'stream' : 'nonstream'}
                    onChange={(_, v) => v && set({ stream: v === 'stream' })}
                    sx={railGroupStyle}
                >
                    <ToggleButton value="nonstream">{t('probe.nonstream')}</ToggleButton>
                    <ToggleButton value="stream">{t('probe.stream')}</ToggleButton>
                </ToggleButtonGroup>
            </Axis>

            <Axis label={t('probe.scope')} hint={scopeHint}>
                <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={axes.direct ? 'direct' : 'tb'}
                    onChange={(_, v) => v && set({ direct: v === 'direct' })}
                    sx={railGroupStyle}
                    disabled={scopeDisabled}
                >
                    <ToggleButton value="tb">{t('probe.throughTB')}</ToggleButton>
                    <ToggleButton value="direct">{t('probe.direct')}</ToggleButton>
                </ToggleButtonGroup>
            </Axis>

            {/* Everything else is advanced — collapsed out of the way until asked for. */}
            <Box>
                <Box
                    sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 0.5,
                        cursor: 'pointer',
                        color: 'text.secondary',
                        '&:hover': { color: 'text.primary' },
                    }}
                    onClick={() => setAdvancedOpen(!advancedOpen)}
                >
                    <Typography variant="caption" sx={{ fontWeight: 500 }}>
                        {t('probe.advanced')}
                    </Typography>
                    {advancedOpen ? <ExpandLessIcon sx={{ fontSize: 16 }} /> : <ExpandMoreIcon sx={{ fontSize: 16 }} />}
                </Box>
                <Collapse in={advancedOpen}>
                    <Stack spacing={1.5} sx={{ mt: 0.5 }}>
                        <Axis label={t('probe.tool')} hint={t('probe.toolHint')}>
                            <ToggleButtonGroup
                                size="small"
                                exclusive
                                value={axes.tool ? 'on' : 'off'}
                                onChange={(_, v) => v && set({ tool: v === 'on' })}
                                sx={railGroupStyle}
                            >
                                <ToggleButton value="off">{t('probe.toolOff')}</ToggleButton>
                                <ToggleButton value="on">{t('probe.toolOn')}</ToggleButton>
                            </ToggleButtonGroup>
                        </Axis>

                        {/* Thinking as a stepped control bar: the effort is a ladder, and a
                            marked slider reads as one knob instead of four buttons. */}
                        <Axis label={t('probe.thinking')} hint={t('probe.thinkingHint')}>
                            <Slider
                                size="small"
                                value={THINKING_LADDER.indexOf(axes.thinking)}
                                min={0}
                                max={THINKING_LADDER.length - 1}
                                step={null}
                                marks={THINKING_LADDER.map((lvl, i) => ({
                                    value: i,
                                    label: t(`probe.thinking${lvl.charAt(0).toUpperCase()}${lvl.slice(1)}`),
                                }))}
                                onChange={(_, v) => set({ thinking: THINKING_LADDER[v as number] })}
                                sx={{
                                    '& .MuiSlider-markLabel': { fontSize: '0.7rem' },
                                }}
                            />
                        </Axis>

                        <Axis
                            label={t('probe.protocol')}
                            hint={
                                protocol.locked || protocol.disabled
                                    ? protocol.lockHint
                                    : `${PROTOCOL_FULL[protocol.value] || ''} · ${t('probe.protocolHint')}`
                            }
                        >
                            <ToggleButtonGroup
                                size="small"
                                exclusive
                                value={protocol.value}
                                onChange={(_, v) => v && set({ protocol: v as ProbeProtocol })}
                                sx={railGroupStyle}
                                disabled={protocol.locked || protocol.disabled}
                            >
                                {(protocol.options.length ? protocol.options : [protocol.value]).map((p) => (
                                    <ToggleButton key={p} value={p}>
                                        {PROTOCOL_LABELS[p] || p}
                                    </ToggleButton>
                                ))}
                            </ToggleButtonGroup>
                        </Axis>

                        {/* Message override lives here — the default per-tool message is
                            right for most probes; custom text is an explicit choice. */}
                        <Axis label={t('probe.message')} hint={t('probe.messageHint')}>
                            <TextField
                                size="small"
                                value={message}
                                onChange={(e) => onMessageChange(e.target.value)}
                                placeholder={messagePlaceholder}
                                multiline
                                maxRows={4}
                                slotProps={{
                                    htmlInput: { sx: { fontSize: '0.78rem' } },
                                    input: { sx: { py: 0.65 } },
                                }}
                                sx={{ width: '100%' }}
                            />
                        </Axis>
                    </Stack>
                </Collapse>
            </Box>
        </Stack>
    );
};

export default ProbeControls;
