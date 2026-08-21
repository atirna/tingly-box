import React, { memo } from 'react';
import { Box, Typography, Tooltip, ToggleButton, ToggleButtonGroup, TextField, Stack } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { toggleButtonGroupStyle } from '@/styles/toggleStyles';
import type { ProbeThinking, ProbeProtocol } from '@/types/probe';
import type { ProbeAxes } from './probeConfig';

// ProbeControls renders the control rail: orthogonal axes stacked vertically,
// one label + control pair per row. The rail sits beside the results pane so
// the controls read as an instrument panel while the results keep the visual
// anchor. Adding a future axis = one more row here (and a field on ProbeAxes).

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

export const PROTOCOL_LABELS: Partial<Record<ProbeProtocol | '', string>> = {
    openai_chat: 'OpenAI Chat',
    openai_responses: 'OpenAI Resp.',
    anthropic_v1: 'Anthropic',
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
            <Box sx={{ mt: 0.5, display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>{children}</Box>
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

    const set = (patch: Partial<ProbeAxes>) => onAxesChange({ ...axes, ...patch });

    return (
        <Stack spacing={1.5}>
            <Axis label={t('probe.shape')} hint={t('probe.shapeHint')}>
                <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={axes.stream ? 'stream' : 'nonstream'}
                    onChange={(_, v) => v && set({ stream: v === 'stream' })}
                    sx={toggleButtonGroupStyle}
                >
                    <ToggleButton value="nonstream">{t('probe.nonstream')}</ToggleButton>
                    <ToggleButton value="stream">{t('probe.stream')}</ToggleButton>
                </ToggleButtonGroup>
            </Axis>

            <Axis label={t('probe.tool')} hint={t('probe.toolHint')}>
                <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={axes.tool ? 'on' : 'off'}
                    onChange={(_, v) => v && set({ tool: v === 'on' })}
                    sx={toggleButtonGroupStyle}
                >
                    <ToggleButton value="off">{t('probe.toolOff')}</ToggleButton>
                    <ToggleButton value="on">{t('probe.toolOn')}</ToggleButton>
                </ToggleButtonGroup>
            </Axis>

            <Axis label={t('probe.thinking')} hint={t('probe.thinkingHint')}>
                <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={axes.thinking}
                    onChange={(_, v) => v && set({ thinking: v as ProbeThinking })}
                    sx={toggleButtonGroupStyle}
                >
                    {(['none', 'low', 'medium', 'high'] as ProbeThinking[]).map((lvl) => (
                        <ToggleButton key={lvl} value={lvl}>
                            {t(`probe.thinking${lvl.charAt(0).toUpperCase()}${lvl.slice(1)}`)}
                        </ToggleButton>
                    ))}
                </ToggleButtonGroup>
            </Axis>

            <Axis
                label={t('probe.protocol')}
                hint={protocol.locked || protocol.disabled ? protocol.lockHint : t('probe.protocolHint')}
            >
                <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={protocol.value}
                    onChange={(_, v) => v && set({ protocol: v as ProbeProtocol })}
                    sx={toggleButtonGroupStyle}
                    disabled={protocol.locked || protocol.disabled}
                >
                    {(protocol.options.length ? protocol.options : [protocol.value]).map((p) => (
                        <ToggleButton key={p} value={p}>
                            {PROTOCOL_LABELS[p] || p}
                        </ToggleButton>
                    ))}
                </ToggleButtonGroup>
            </Axis>

            <Axis label={t('probe.scope')} hint={scopeHint}>
                <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={axes.direct ? 'direct' : 'tb'}
                    onChange={(_, v) => v && set({ direct: v === 'direct' })}
                    sx={toggleButtonGroupStyle}
                    disabled={scopeDisabled}
                >
                    <ToggleButton value="tb">{t('probe.throughTB')}</ToggleButton>
                    <ToggleButton value="direct">{t('probe.direct')}</ToggleButton>
                </ToggleButtonGroup>
            </Axis>

            <Axis label={t('probe.message')} hint={t('probe.messageHint')}>
                <TextField
                    size="small"
                    value={message}
                    onChange={(e) => onMessageChange(e.target.value)}
                    placeholder={messagePlaceholder}
                    multiline
                    maxRows={3}
                    slotProps={{
                        htmlInput: { sx: { fontSize: '0.78rem' } },
                        input: { sx: { py: 0.65 } },
                    }}
                    sx={{ width: '100%' }}
                />
            </Axis>
        </Stack>
    );
};

export default ProbeControls;
