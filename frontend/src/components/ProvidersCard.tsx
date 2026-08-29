import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
    Alert,
    Button,
    Dialog,
    DialogActions,
    DialogContent,
    DialogContentText,
    DialogTitle,
    Snackbar,
} from '@mui/material';
import UnifiedCard from '@/components/UnifiedCard.tsx';
import ConnectAIDialogs from '@/components/ConnectAIDialogs';
import { ProviderListContent } from '@/components/ConnectProviderDialog';
import { useProviderDialog } from '@/hooks/useProviderDialog';

/**
 * ProvidersCard — browse the provider catalog and connect one, inline as a
 * card rather than a full page. This is what the old standalone Onboarding
 * page used to be: the lightbulb Help entry is the new onboarding front
 * door, and this card is the one piece of Onboarding's content that still
 * needed a home (the shared Connect AI *dialog* — form/OAuth/paste/import —
 * already exists everywhere else; this card is specifically the *browsable
 * list* view of it).
 *
 * Unlike CredentialPage's Connect AI (a picker hidden behind a button, so
 * empty-state entry points need a `?dialog=add` deep link to open it), the
 * list here is always visible as soon as the card renders — first-run and
 * empty-state entry points just need to land on this page, no query param
 * needed.
 */
export const ProvidersCard = () => {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const [browseQuery, setBrowseQuery] = useState('');

    const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' | 'info' }>({
        open: false,
        message: '',
        severity: 'info',
    });
    const [successDialogOpen, setSuccessDialogOpen] = useState(false);

    const showMessage = (message: string, severity: 'success' | 'error' | 'info' = 'info') => {
        setSnackbar({ open: true, message, severity });
    };

    // The same Connect AI flow every other surface uses: the provider list is
    // rendered inline (card content instead of a picker dialog), and every
    // card — key / custom / self-hosted / OAuth / import / paste & detect —
    // routes through the shared hook + dialog stack.
    const connectAI = useProviderDialog(showMessage, {
        onProviderAdded: () => setSuccessDialogOpen(true),
    });

    const handleGoToAgents = () => {
        setSuccessDialogOpen(false);
        navigate('/agent');
    };

    const handleStayHere = () => {
        setSuccessDialogOpen(false);
        showMessage(t('onboarding.success', { defaultValue: 'Provider added successfully! You can now create scenarios.' }), 'success');
    };

    return (
        <>
            <UnifiedCard
                size="full"
                title={t('help.providers.title')}
                subtitle={t('help.providers.description')}
            >
                <ProviderListContent
                    onSelect={connectAI.handleConnectSelect}
                    query={browseQuery}
                    onQueryChange={setBrowseQuery}
                    hideOfficialInfo={true}
                    showDetails={true}
                    wide={true}
                />
            </UnifiedCard>

            {/* Shared Connect AI dialog stack (form / OAuth / paste / import /
                cloud). inline: the provider list above is the picker, so no
                picker dialog and no "← Back to picker" in the form. */}
            <ConnectAIDialogs flow={connectAI} inline isFirstProvider />

            <Dialog
                open={successDialogOpen}
                onClose={() => setSuccessDialogOpen(false)}
                aria-labelledby="providers-success-dialog-title"
                aria-describedby="providers-success-dialog-description"
            >
                <DialogTitle id="providers-success-dialog-title">
                    {t('onboarding.dialog.title', { defaultValue: 'Provider Added' })}
                </DialogTitle>
                <DialogContent>
                    <DialogContentText id="providers-success-dialog-description">
                        {t('onboarding.dialog.message', { defaultValue: 'Your AI provider has been added successfully. Would you like to go to the agents page to start using it?' })}
                    </DialogContentText>
                </DialogContent>
                <DialogActions>
                    <Button onClick={handleStayHere}>
                        {t('onboarding.dialog.stay', { defaultValue: 'Stay Here' })}
                    </Button>
                    <Button onClick={handleGoToAgents} variant="contained" autoFocus>
                        {t('onboarding.dialog.goToAgents', { defaultValue: 'Go to Agents' })}
                    </Button>
                </DialogActions>
            </Dialog>

            <Snackbar
                open={snackbar.open}
                autoHideDuration={snackbar.severity === 'error' ? null : 4000}
                onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
            >
                <Alert
                    severity={snackbar.severity}
                    onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}
                >
                    {snackbar.message}
                </Alert>
            </Snackbar>
        </>
    );
};

export default ProvidersCard;
