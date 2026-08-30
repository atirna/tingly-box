import { Box, Dialog, DialogActions, DialogContent, DialogTitle, Button, Typography, Stack } from '@mui/material';
import React from 'react';
import { useScenarioPageModal } from '@/pages/scenario/context/ScenarioPageContext';

interface CursorConfigModalProps {
    open: boolean;
    onClose: () => void;
    baseUrl: string;
    copyToClipboard: (text: string, label: string) => Promise<void>;
}

const CursorConfigModal: React.FC<CursorConfigModalProps> = ({
    open,
    onClose,
    baseUrl,
    copyToClipboard,
}) => {
    // Get token from context
    const { token } = useScenarioPageModal();
    return (
        <Dialog
            open={open}
            onClose={onClose}
            maxWidth="sm"
            fullWidth
            slotProps={{
                paper: {
                    sx: {
                        borderRadius: 3,
                    }
                }
            }}
        >
            <DialogTitle sx={{ pb: 1 }}>
                <Typography variant="h6" sx={{
                    fontWeight: 600
                }}>
                    Configure Cursor
                </Typography>
            </DialogTitle>
            <DialogContent sx={{ pt: 1 }}>
                <Stack spacing={2}>
                    <Box sx={{ bgcolor: 'background.paper', p: 2, borderRadius: 1, border: 1, borderColor: 'divider' }}>
                        <Typography variant="subtitle2" sx={{ mb: 1.5 }}>
                            <strong>1.</strong> Open <strong>Cursor</strong> → <strong>Settings</strong> → <strong>Models</strong>
                        </Typography>
                        <Typography variant="subtitle2" sx={{ mb: 1.5 }}>
                            <strong>2.</strong> Under <strong>OpenAI API Key</strong>, enable <strong>Override OpenAI Base URL</strong>
                        </Typography>
                        <Typography variant="subtitle2" sx={{ mb: 1 }}>
                            <strong>3.</strong> Enter:
                        </Typography>
                        <Box sx={{ pl: 2, mb: 0.5 }}>
                            <Typography variant="subtitle2" sx={{ fontFamily: 'monospace' }}>
                                Base URL: <strong>{baseUrl}/tingly/cursor</strong>
                            </Typography>
                            <Typography variant="subtitle2" sx={{ fontFamily: 'monospace' }}>
                                API Key: <strong>{token.slice(0, 16)}...</strong>
                            </Typography>
                        </Box>
                        <Typography variant="subtitle2" sx={{ mt: 1.5 }}>
                            <strong>4.</strong> Click <strong>Verify</strong> to save
                        </Typography>
                    </Box>

                    <Stack direction="row" spacing={1}>
                        <Button
                            variant="outlined"
                            size="small"
                            onClick={() => copyToClipboard(`${baseUrl}/tingly/cursor`, 'URL')}
                            sx={{ flex: 1 }}
                        >
                            Copy URL
                        </Button>
                        <Button
                            variant="outlined"
                            size="small"
                            onClick={() => copyToClipboard(token, 'API Key')}
                            sx={{ flex: 1 }}
                        >
                            Copy API Key
                        </Button>
                    </Stack>
                </Stack>
            </DialogContent>
            <DialogActions sx={{ px: 3, pb: 2, pt: 1 }}>
                <Button onClick={onClose} variant="contained">
                    Done
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default CursorConfigModal;
