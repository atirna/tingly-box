import { Check as IconCheck, KeyboardArrowDown as IconChevronDown } from '@/components/icons';
import {
    Box,
    Button,
    Dialog,
    DialogContent,
    DialogTitle,
    ListItemText,
    Menu,
    MenuItem,
    Tooltip,
    Typography,
} from '@mui/material';
import React, { useState } from 'react';
import ModelSelectDialog from '../ModelSelectDialog';
import type { ProviderSelectTabOption } from '../ModelSelectDialog';
import type { Provider } from '@/types/provider';

/** A `{provider, model}` pair — the value shape of every service_ref flag. */
export interface ServiceRef {
    provider: string;
    model: string;
}

export interface ServiceRefControlProps {
    /** Short feature name shown on the button, e.g. "Vision Proxy". */
    name: string;
    /** Tooltip shown when nothing is configured — say what the feature does. */
    offHint: string;
    /** Tooltip when configured. Receives the resolved provider name + model. */
    onHint: (providerName: string, model: string) => string;
    /** Secondary line under "On" in the menu when nothing is picked yet. */
    pickHint: string;
    value: ServiceRef | null;
    providers: Provider[];
    disabled?: boolean;
    onChange: (service: ServiceRef | null) => void;
}

/**
 * The shared control for a `{provider, model}` feature: one button that is both
 * the on/off switch and the current-model display.
 *
 * There is deliberately no separate toggle. "Enabled" is exactly "a service is
 * configured", so a toggle beside a model picker would be a second source of
 * truth the two could drift apart on. Picking a model turns the feature on;
 * choosing Off clears it.
 *
 * Vision Proxy and Web Proxy both render through this, so the two features are
 * one thing to learn rather than two.
 */
const ServiceRefControl: React.FC<ServiceRefControlProps> = ({
    name,
    offHint,
    onHint,
    pickHint,
    value,
    providers,
    disabled,
    onChange,
}) => {
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    const [pickerOpen, setPickerOpen] = useState(false);

    const providerName = (uuid: string) => providers.find(p => p.uuid === uuid)?.name || uuid;
    const isEnabled = !!value;
    const label = isEnabled ? value!.model : 'Off';
    const tooltip = isEnabled ? onHint(providerName(value!.provider), value!.model) : offHint;

    return (
        <>
            <Tooltip title={tooltip} placement="right" arrow>
                <Button
                    size="small"
                    variant="outlined"
                    onClick={(e) => !disabled && setAnchor(e.currentTarget)}
                    disabled={disabled}
                    endIcon={<IconChevronDown sx={{ fontSize: 18 }} />}
                    sx={{
                        minWidth: 100,
                        maxWidth: 260,
                        textTransform: 'none',
                        whiteSpace: 'nowrap',
                        '& .MuiButton-endIcon': { flexShrink: 0 },
                        bgcolor: isEnabled ? 'primary.main' : 'transparent',
                        color: isEnabled ? 'primary.contrastText' : 'text.primary',
                        fontWeight: isEnabled ? 600 : 400,
                        border: isEnabled ? 'none' : '1px solid',
                        borderColor: 'divider',
                        opacity: disabled ? 0.6 : 1,
                        '&:hover': { bgcolor: isEnabled ? 'primary.dark' : 'action.selected' },
                    }}
                >
                    <Box component="span" sx={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>
                        {name}: {label}
                    </Box>
                </Button>
            </Tooltip>
            <Menu
                anchorEl={anchor}
                open={Boolean(anchor)}
                onClose={() => setAnchor(null)}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
                transformOrigin={{ vertical: 'top', horizontal: 'left' }}
            >
                <MenuItem
                    selected={!isEnabled}
                    onClick={() => { setAnchor(null); if (isEnabled) onChange(null); }}
                >
                    <ListItemText primary="Off" slotProps={{ primary: { variant: 'body2' } }} />
                    {!isEnabled && <IconCheck sx={{ fontSize: 16 }} />}
                </MenuItem>
                <MenuItem
                    selected={isEnabled}
                    onClick={() => { setAnchor(null); setPickerOpen(true); }}
                >
                    <ListItemText
                        primary={isEnabled ? `On — ${value!.model}` : 'On — pick a model…'}
                        secondary={isEnabled ? providerName(value!.provider) : pickHint}
                        slotProps={{ primary: { variant: 'body2' }, secondary: { variant: 'caption' } }}
                    />
                    {isEnabled && <IconCheck sx={{ fontSize: 16 }} />}
                </MenuItem>
            </Menu>
            <Dialog
                open={pickerOpen}
                onClose={() => setPickerOpen(false)}
                maxWidth="lg"
                fullWidth
                slotProps={{
                    paper: { sx: { height: '80vh' } }
                }}
            >
                <DialogTitle sx={{ textAlign: 'center' }}>
                    <Typography variant="h6">Pick {name} Model</Typography>
                </DialogTitle>
                <DialogContent>
                    <ModelSelectDialog
                        providers={providers}
                        selectedProvider={value?.provider}
                        selectedModel={value?.model}
                        onSelected={async (option: ProviderSelectTabOption) => {
                            onChange({ provider: option.provider.uuid, model: option.model });
                            setPickerOpen(false);
                        }}
                        onSelectionClear={async () => {
                            onChange(null);
                            setPickerOpen(false);
                        }}
                    />
                </DialogContent>
            </Dialog>
        </>
    );
};

export default ServiceRefControl;
