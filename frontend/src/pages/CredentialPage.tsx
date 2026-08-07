import ConnectAIDialogs from '@/components/ConnectAIDialogs';
import CredentialTable from '@/components/CredentialTable.tsx';
import EmptyState from '@/components/EmptyState';
import OAuthDialog from '@/components/OAuthDialog.tsx';
import PageHeader from '@/components/PageHeader';
import { PageLayout } from '@/components/PageLayout';
import Surface from '@/components/Surface';
import { useProviderQuota } from '@/hooks/useProviderQuota';
import { useProviderEditDialog } from '@/hooks/useProviderEditDialog';
import { useProviderDialog } from '@/hooks/useProviderDialog';
import { Add, ListAlt, VpnKey } from '@/components/icons';
import {
    Alert,
    Button,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    Stack,
    Typography,
} from '@mui/material';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../services/api';
import { useNotify } from '@/hooks/useNotify';

const CredentialPage = () => {
    const [searchParams, setSearchParams] = useSearchParams();
    const [providers, setProviders] = useState<any[]>([]);
    const [loading, setLoading] = useState(true);
    const notify = useNotify();

    // Reauthorize dialog state (page-local: re-authenticates an existing OAuth
    // provider in place — the shared Connect AI flow only covers adding).
    const [oauthDialogOpen, setOAuthDialogOpen] = useState(false);
    const [oauthAutoStartId, setOAuthAutoStartId] = useState<string | null>(null);
    const [oauthReauthUuid, setOAuthReauthUuid] = useState<string | null>(null);
    const [refreshFailPrompt, setRefreshFailPrompt] = useState<{
        open: boolean;
        providerUuid: string;
        providerName: string;
        reason: string;
    }>({ open: false, providerUuid: '', providerName: '', reason: '' });

    useEffect(() => { loadProviders(); }, []);

    const { quotaData, refreshing, refreshQuota } = useProviderQuota(providers, { fetchOnMount: true });

    const showNotification = useCallback((message: string, severity: 'success' | 'error') => {
        notify[severity](message);
    }, [notify]);

    // Standard "Connect AI" add flow: picker + every downstream dialog
    // (form / OAuth / paste / import) via the shared hook + ConnectAIDialogs.
    const connectAI = useProviderDialog(showNotification, {
        onProviderAdded: () => loadProviders(),
    });
    const { handleConnectAIClick } = connectAI;

    const loadProviders = async () => {
        setLoading(true);
        const result = await api.getProviders();
        if (result.success) { setProviders(result.data); }
        else { showNotification(`Failed to load providers: ${result.error}`, 'error'); }
        setLoading(false);
    };

    const { editProvider: handleEditProvider, providerEditDialogs } = useProviderEditDialog({
        showNotification,
        onUpdated: loadProviders,
    });

    const handleDeleteProvider = async (uuid: string) => {
        const result = await api.deleteProvider(uuid);
        if (result.success) { showNotification('Provider deleted successfully!', 'success'); loadProviders(); }
        else { showNotification(`Failed to delete provider: ${result.error}`, 'error'); }
    };

    const handleToggleProvider = async (uuid: string) => {
        const result = await api.toggleProvider(uuid);
        if (result.success) { showNotification(result.message, 'success'); loadProviders(); }
        else { showNotification(`Failed to toggle provider: ${result.error}`, 'error'); }
    };


    // URL param handling for auto-opening dialogs
    useEffect(() => {
        const editProvider = searchParams.get('editProvider');
        if (editProvider) {
            const nextParams = new URLSearchParams(searchParams);
            nextParams.delete('editProvider');
            setSearchParams(nextParams, { replace: true });
            handleEditProvider(editProvider);
            return;
        }

        const dialog = searchParams.get('dialog');
        if (dialog === 'add') {
            const nextParams = new URLSearchParams(searchParams);
            nextParams.delete('dialog');
            setSearchParams(nextParams, { replace: true });
            // All "add credential" entry points funnel through the unified Connect AI picker.
            handleConnectAIClick();
        }
    }, [searchParams, setSearchParams, handleConnectAIClick]);

    // Reauthorize handlers (add-flow OAuth success is handled by the shared hook)
    const handleReauthSuccess = () => {
        showNotification('Provider reauthorized successfully!', 'success');
        setOAuthReauthUuid(null);
        loadProviders();
    };

    const handleReauthorize = (providerUuid: string) => {
        const provider = oauthProviders.find((p: any) => p.uuid === providerUuid);
        const issuer = provider?.oauth_detail?.provider_type || provider?.oauth_detail?.issuer;
        if (!issuer) { showNotification('Cannot reauthorize: provider issuer is unknown', 'error'); return; }
        setOAuthReauthUuid(providerUuid);
        setOAuthAutoStartId(issuer);
        setOAuthDialogOpen(true);
    };

    const promptReauthAfterRefreshFailure = (providerUuid: string, reason: string) => {
        const provider = oauthProviders.find((p: any) => p.uuid === providerUuid);
        setRefreshFailPrompt({ open: true, providerUuid, providerName: provider?.name || 'this provider', reason: reason || 'Unknown error' });
    };

    const handleRefreshToken = async (providerUuid: string) => {
        try {
            const response = await api.oauthRefresh({ provider_uuid: providerUuid });
            if (response?.success) { showNotification('Token refreshed successfully!', 'success'); await loadProviders(); }
            else { promptReauthAfterRefreshFailure(providerUuid, response?.data?.error || response?.error || response?.message || 'Unknown error'); }
        } catch (error: any) {
            promptReauthAfterRefreshFailure(providerUuid, error?.response?.data?.error || error?.message || 'Unknown error');
        }
    };

    // credentialProviders drives the unified table: every real credential
    // (OAuth + static keys/tokens), excluding virtual models which have no
    // credential of their own. oauthProviders stays separate because the
    // reauthorize handlers below only ever need to look up OAuth providers.
    const { credentialProviders, oauthProviders, credentialCounts } = useMemo(() => {
        const creds = providers.filter((p: any) => p.auth_type !== 'vmodel');
        const oauth = creds.filter((p: any) => p.auth_type === 'oauth');
        return {
            credentialProviders: creds,
            oauthProviders: oauth,
            credentialCounts: { oauth: oauth.length, apiKeys: creds.length - oauth.length, total: creds.length },
        };
    }, [providers]);

    return (
        <PageLayout loading={loading}>
            <Stack spacing={2.5}>
                <PageHeader
                    title="Credentials"
                    subtitle={`Managing ${credentialCounts.total} credential${credentialCounts.total !== 1 ? 's' : ''}`}
                    actions={
                        <Stack
                            direction="row"
                            spacing={1}
                            useFlexGap
                            sx={{
                                flexWrap: "wrap",
                                justifyContent: { xs: 'flex-start', sm: 'flex-end' }
                            }}>
                            <Button component={Link} to="/credentials/providers" variant="outlined" startIcon={<ListAlt />} size="small" sx={{ minWidth: 130 }}>Providers</Button>
                            <Button variant="contained" startIcon={<Add />} onClick={handleConnectAIClick} size="small" sx={{ minWidth: 150 }}>Connect AI</Button>
                        </Stack>
                    }
                />

                {/* Credentials — OAuth sign-ins and static API keys/tokens in one
                    table, grouped and sorted by mechanism rather than split
                    into separate tables/cards (auth type is an attribute of
                    a credential, not a top-level category). */}
                <Surface padding={{ xs: 2, sm: 2.5 }}>
                    {credentialCounts.total > 0 ? (
                        <CredentialTable providers={credentialProviders} onEdit={handleEditProvider} onToggle={handleToggleProvider} onDelete={handleDeleteProvider} onRefreshToken={handleRefreshToken} onReauthorize={handleReauthorize} onNotification={showNotification} providerQuotas={quotaData} refreshingQuotas={refreshing} onQuotaRefresh={refreshQuota}/>
                    ) : (
                        <EmptyState
                            title="No Credentials Configured"
                            description="Connect AI providers like OpenAI, Anthropic, Claude Code, Gemini CLI, etc. via API key or OAuth sign-in."
                            primaryAction={{ label: 'Connect AI', onClick: handleConnectAIClick }}
                        />
                    )}
                </Surface>
            </Stack>
            {/* Unified Connect AI add flow: picker + form/OAuth/paste/import dialogs
                (edit goes through useProviderEditDialog) */}
            <ConnectAIDialogs flow={connectAI}/>
            {/* Reauthorize dialog (existing OAuth provider, in place) */}
            <OAuthDialog open={oauthDialogOpen} autoStartProviderId={oauthAutoStartId} reauthProviderUuid={oauthReauthUuid} onClose={() => { setOAuthDialogOpen(false); setOAuthAutoStartId(null); setOAuthReauthUuid(null); }} onSuccess={handleReauthSuccess}/>
            {/* Refresh-failed → reauthorize guidance */}
            <Dialog open={refreshFailPrompt.open} onClose={() => setRefreshFailPrompt((s) => ({ ...s, open: false }))} maxWidth="xs" fullWidth>
                <DialogTitle>Token refresh failed</DialogTitle>
                <DialogContent>
                    <Stack spacing={2} sx={{ pt: 0.5 }}>
                        <Alert severity="warning">{refreshFailPrompt.reason}</Alert>
                        <Typography variant="body2" sx={{
                            color: "text.secondary"
                        }}>
                            Refreshing the token for <strong>{refreshFailPrompt.providerName}</strong> didn't work. If the credential was revoked or has fully expired, a refresh can't recover it — reauthorize to sign in again. This overwrites the credential in place, keeping the same provider so your routing rules and model keys stay intact.
                        </Typography>
                    </Stack>
                </DialogContent>
                <DialogActions>
                    <Button color="inherit" onClick={() => setRefreshFailPrompt((s) => ({ ...s, open: false }))}>Dismiss</Button>
                    <Button variant="contained" startIcon={<VpnKey />} onClick={() => { const uuid = refreshFailPrompt.providerUuid; setRefreshFailPrompt((s) => ({ ...s, open: false })); handleReauthorize(uuid); }}>Reauthorize</Button>
                </DialogActions>
            </Dialog>
            {providerEditDialogs}
        </PageLayout>
    );
};

export default CredentialPage;
