import React, { memo } from 'react';
import { Box, Typography, Tooltip, ToggleButton, ToggleButtonGroup, TextField, Grid } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { toggleButtonGroupStyle } from '@/styles/toggleStyles';
import type { ProbeThinking, ProbeProtocol } from '@/types/probe';
import type { ProbeAxes } from './probeConfig';

// ProbeControls renders the Request Config block: orthogonal axis rows in a
// two-column grid. Adding a future axis = a new row here (and a field on
// ProbeAxes), not layout surgery elsewhere.

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

const AxisLabel = memo(({ label, hint }: { label: string; hint?: string }) => {
    const body = (
        <Typography
            variant="caption"
            sx={{ color: 'text.secondary', width: 68, flexShrink: 0, cursor: hint ? 'help' : 'default' }}
        >
            {label}
        </Typography>
    );
    return hint ? <Tooltip title={hint}>{body}</Tooltip> : body;
});

const AxisRow = memo(({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
        <AxisLabel label={label} hint={hint} />
        <Box sx={{ flex: 1, minWidth: 0, display: 'flex', justifyContent: 'flex-start' }}>{children}</Box>
    </Box>
));

export const PROTOCOL_LABELS: Partial<Record<ProbeProtocol | '', string>> = {
    openai_chat: 'OpenAI Chat',
    openai_responses: 'OpenAI Resp.',
    anthropic_v1: 'Anthropic',
};

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
        <Box
            sx={{
                border: '1px solid',
                borderColor: 'divider',
                borderRadius: 1.5,
                p: 1.5,
            }}
        >
            <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1.5 }}>
                {t('probe.requestConfig')}
            </Typography>
            <Grid container spacing={1.5}>
                <Grid item xs={12} sm={6} {...({ item: true } as any)}>
                    <AxisRow label={t('probe.shape')} hint={t('probe.shapeHint')}>
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
                    </AxisRow>
                </Grid>
                <Grid item xs={12} sm={6} {...({ item: true } as any)}>
                    <AxisRow label={t('probe.tool')} hint={t('probe.toolHint')}>
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
                    </AxisRow>
                </Grid>
                <Grid item xs={12} sm={6} {...({ item: true } as any)}>
                    <AxisRow label={t('probe.thinking')} hint={t('probe.thinkingHint')}>
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
                    </AxisRow>
                </Grid>
                <Grid item xs={12} sm={6} {...({ item: true } as any)}>
                    <AxisRow
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
                    </AxisRow>
                </Grid>
                <Grid item xs={12} sm={6} {...({ item: true } as any)}>
                    <AxisRow label={t('probe.scope')} hint={scopeHint}>
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
                    </AxisRow>
                </Grid>
                <Grid item xs={12} sm={6} {...({ item: true } as any)}>
                    <AxisRow label={t('probe.message')} hint={t('probe.messageHint')}>
                        <TextField
                            size="small"
                            value={message}
                            onChange={(e) => onMessageChange(e.target.value)}
                            placeholder={messagePlaceholder}
                            slotProps={{ htmlInput: { sx: { fontSize: '0.78rem' } } }}
                            sx={{ width: '100%' }}
                        />
                    </AxisRow>
                </Grid>
            </Grid>
        </Box>
    );
};

export default ProbeControls;
