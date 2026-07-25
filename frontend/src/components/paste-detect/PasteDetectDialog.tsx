import {
    Dialog,
    DialogContent,
    DialogTitle,
    IconButton,
    Stack,
    Typography,
} from '@mui/material';
import {ArrowBack, Close} from '@/components/icons';
import PasteDetectPanel from './PasteDetectPanel';
import type {EnhancedProviderFormData} from '@/components/ProviderFormDialog';
import {emptyForm} from '@/hooks/useProviderDialog';

// Dialog shell for the Connect AI "Paste & detect" path. Wraps the shared
// PasteDetectPanel (the same core Onboarding uses inline) so the experience is
// identical across surfaces.
//
// NOTE: detection calls POST /api/v1/onboarding/extract, which is only served by
// the real backend. In MSW / mock mode there is no handler, so Detect will fail
// and surface the panel's error UI — this is expected, not a bug.

export interface PasteDetectDialogProps {
    open: boolean;
    /** Close without producing a prefill (Back / X / Escape / backdrop). */
    onClose: () => void;
    /** Close after the user picked (or chose to fill manually) — parent opens the form. */
    onPick: (prefill: EnhancedProviderFormData) => void;
}

const PasteDetectDialog: React.FC<PasteDetectDialogProps> = ({open, onClose, onPick}) => {
    return (
        <Dialog
            open={open}
            onClose={onClose}
            aria-labelledby="paste-detect-dialog-title"
            maxWidth="sm"
            fullWidth
            scroll="paper"
            slotProps={{
                paper: {sx: {maxHeight: '88vh', display: 'flex', flexDirection: 'column'}},
            }}
        >
            {/* Locked header: Back + title + close never scroll. */}
            <DialogTitle id="paste-detect-dialog-title" sx={{pb: 1, flexShrink: 0}}>
                <Stack
                    direction="row"
                    sx={{
                        alignItems: "center",
                        justifyContent: "space-between",
                        gap: 1,
                    }}>
                    <Stack direction="row" sx={{alignItems: "center", gap: 1}}>
                        <IconButton aria-label="Back to provider picker" onClick={onClose} size="small">
                            <ArrowBack/>
                        </IconButton>
                        <Typography component="span" variant="h6">Paste &amp; detect</Typography>
                    </Stack>
                    <IconButton aria-label="Close Paste and detect" onClick={onClose} size="small"><Close/></IconButton>
                </Stack>
            </DialogTitle>
            <DialogContent
                dividers
                sx={{
                    pt: 2,
                    flex: 1,
                    overflowY: 'hidden', // Panel handles its own scrolling
                }}
            >
                <PasteDetectPanel
                    onPick={onPick}
                    onManualFill={() => onPick(emptyForm())}
                />
            </DialogContent>
        </Dialog>
    );
};

export default PasteDetectDialog;
