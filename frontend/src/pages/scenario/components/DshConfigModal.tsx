import { Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Tab, Tabs, Tooltip, Typography } from '@mui/material';
import React from 'react';
import { useTranslation } from 'react-i18next';
import { RestartAlt } from '@/components/icons';
import CodeBlock from '@/components/CodeBlock';
import DshQuickConfig, { type DshPrefs, defaultDshPrefs, mergeSavedDshPrefs } from './DshQuickConfig';
import { shouldIgnoreDialogClose } from '@/components/dialogClose';
import { api } from '@/services/api';
import { useScenarioPageModal } from '@/pages/scenario/context/ScenarioPageContext';

interface DshConfigModalProps {
    open: boolean;
    onClose: () => void;
    copyToClipboard: (text: string, label: string) => Promise<void>;
    // Shared page-level toast, used for apply success/error so feedback is
    // consistent with the rest of the scenario pages.
    showNotification?: (message: string, severity: 'success' | 'error' | 'info' | 'warning') => void;
}

type MainTab = 'quick' | 'manual';
type ScriptTab = 'yaml' | 'windows' | 'unix';

const DSH_API_KEY_ENV = 'TINGLY_BOX_API_KEY';

const DshConfigModal: React.FC<DshConfigModalProps> = ({
    open,
    onClose,
    copyToClipboard,
    showNotification,
}) => {
    const { t } = useTranslation();
    // Fallback for the credentials.yaml preview while the preview request is
    // in flight.
    const { token } = useScenarioPageModal();
    const [mainTab, setMainTab] = React.useState<MainTab>('quick');
    const [prefs, setPrefs] = React.useState<DshPrefs>(() => defaultDshPrefs());
    const [settingsTab, setSettingsTab] = React.useState<ScriptTab>('yaml');
    const [credsTab, setCredsTab] = React.useState<ScriptTab>('yaml');
    const [settingsYaml, setSettingsYaml] = React.useState<string>('# Loading...');
    const [credentialsYaml, setCredentialsYaml] = React.useState<string>(`${DSH_API_KEY_ENV}: "${token}"\n`);
    const [previewModels, setPreviewModels] = React.useState<string[]>([]);

    // True while the applied-config readback is in flight, so the Quick tab
    // can show a spinner instead of flashing the defaults before the saved
    // values land.
    const [isConfigLoading, setIsConfigLoading] = React.useState(false);
    const [isApplying, setIsApplying] = React.useState(false);

    // On open, restore the prefs previously applied to
    // $DSH_HOME/settings.yaml; first-time users fall back to defaults.
    React.useEffect(() => {
        if (!open) {
            setPrefs(defaultDshPrefs());
            setIsConfigLoading(false);
            return;
        }
        let active = true;
        setIsConfigLoading(true);
        void api.getAppliedDshConfig().then(result => {
            if (!active) return;
            if (result?.success && result.exists) {
                setPrefs(mergeSavedDshPrefs(result.preferences || {}));
            } else {
                setPrefs(defaultDshPrefs());
            }
        }).finally(() => {
            if (active) setIsConfigLoading(false);
        });
        return () => {
            active = false;
        };
    }, [open]);

    // Re-render the server-authoritative YAML whenever prefs change while the
    // modal is open. Debounced so dragging through the Select doesn't spam
    // the backend.
    React.useEffect(() => {
        if (!open) return;
        let cancelled = false;
        const handle = setTimeout(async () => {
            try {
                const resp = await api.getDshConfigPreview(prefs as Record<string, string>);
                if (cancelled) return;
                if (resp?.success) {
                    setSettingsYaml(resp.settingsYaml || '');
                    setCredentialsYaml(resp.credentialsYaml || `${DSH_API_KEY_ENV}: "${token}"\n`);
                    setPreviewModels(resp.models || []);
                }
            } catch {
                // Leave existing placeholders in place.
            }
        }, 250);
        return () => { cancelled = true; clearTimeout(handle); };
    }, [open, prefs, token]);

    const windowsSettingsScript = `$dshHome = if ($env:DSH_HOME) { $env:DSH_HOME } else { Join-Path $HOME ".dsh" }
$settingsPath = Join-Path $dshHome "settings.yaml"

New-Item -ItemType Directory -Force -Path $dshHome | Out-Null

@'
${settingsYaml}
'@ | Set-Content -Path $settingsPath`;

    const unixSettingsScript = `DSH_HOME="\${DSH_HOME:-$HOME/.dsh}"
mkdir -p "$DSH_HOME"

cat > "$DSH_HOME/settings.yaml" <<'EOF'
${settingsYaml}
EOF`;

    const windowsCredsScript = `$dshHome = if ($env:DSH_HOME) { $env:DSH_HOME } else { Join-Path $HOME ".dsh" }
$credsPath = Join-Path $dshHome ".credentials.yaml"

New-Item -ItemType Directory -Force -Path $dshHome | Out-Null

@'
${credentialsYaml}
'@ | Set-Content -Path $credsPath`;

    const unixCredsScript = `DSH_HOME="\${DSH_HOME:-$HOME/.dsh}"
mkdir -p "$DSH_HOME"

cat > "$DSH_HOME/.credentials.yaml" <<'EOF'
${credentialsYaml}
EOF`;

    const handleApplyConfiguration = async () => {
        setIsApplying(true);
        try {
            const response = await api.applyDshConfig(prefs as Record<string, string>);
            if (response?.success) {
                showNotification?.(t('dshConfig.applySuccess'), 'success');
            } else {
                showNotification?.(response?.message || t('dshConfig.applyFailed'), 'error');
            }
        } catch (err: any) {
            showNotification?.(err?.message || t('dshConfig.applyFailed'), 'error');
        } finally {
            setIsApplying(false);
        }
    };

    return (
        <Dialog
            open={open}
            onClose={(event, reason) => {
                if (shouldIgnoreDialogClose(reason)) {
                    return;
                }
                onClose();
            }}
            maxWidth="lg"
            fullWidth
            slotProps={{
                paper: {
                    sx: {
                        borderRadius: 3,
                        maxHeight: '90vh',
                    },
                }
            }}
        >
            <DialogTitle sx={{ pb: 1, borderBottom: 1, borderColor: 'divider', position: 'relative' }}>
                <Typography variant="h6" sx={{ fontWeight: 600 }}>
                    {t('dshConfig.title')}
                </Typography>
                <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>
                    {t('dshConfig.subtitle')}
                </Typography>
                {mainTab === 'quick' && (
                    <Tooltip title={t('dshConfig.resetTooltip')} arrow>
                        <IconButton
                            size="small"
                            onClick={() => setPrefs(defaultDshPrefs())}
                            sx={{ position: 'absolute', top: 12, right: 12 }}
                        >
                            <RestartAlt fontSize="small" />
                        </IconButton>
                    </Tooltip>
                )}
                <Tabs
                    value={mainTab}
                    onChange={(_, value) => setMainTab(value)}
                    sx={{ mt: 1, minHeight: 40, '& .MuiTabs-indicator': { height: 3 } }}
                >
                    <Tab label={t('dshConfig.tabQuick')} value="quick" sx={{ minHeight: 40, textTransform: 'none' }} />
                    <Tab label={t('dshConfig.tabManual')} value="manual" sx={{ minHeight: 40, textTransform: 'none' }} />
                </Tabs>
            </DialogTitle>
            <DialogContent sx={{ p: 3 }}>
                {mainTab === 'quick' && isConfigLoading && (
                    <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
                        <CircularProgress size={28} />
                    </Box>
                )}

                {mainTab === 'quick' && !isConfigLoading && (
                    <DshQuickConfig prefs={prefs} setPrefs={setPrefs} />
                )}

                {mainTab === 'manual' && (
                    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                        <Box sx={{ display: 'flex', flexDirection: 'column' }}>
                            <Box sx={{ mb: 1, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                                <Typography variant="subtitle2" sx={{ color: 'text.secondary' }}>
                                    Step 1 · Create or update `$DSH_HOME/settings.yaml`
                                </Typography>
                                <Tabs
                                    value={settingsTab}
                                    onChange={(_, value) => setSettingsTab(value)}
                                    variant="standard"
                                    sx={{ minHeight: 32, '& .MuiTabs-indicator': { height: 3 } }}
                                >
                                    <Tab label="YAML" value="yaml" sx={{ minHeight: 32, py: 0.5, fontSize: '0.875rem' }} />
                                    <Tab label="Windows" value="windows" sx={{ minHeight: 32, py: 0.5, fontSize: '0.875rem' }} />
                                    <Tab label="Linux/macOS" value="unix" sx={{ minHeight: 32, py: 0.5, fontSize: '0.875rem' }} />
                                </Tabs>
                            </Box>
                            <Box>
                                {settingsTab === 'yaml' && (
                                    <CodeBlock
                                        code={settingsYaml}
                                        language="yaml"
                                        filename="Create or update $DSH_HOME/settings.yaml"
                                        wrap={true}
                                        onCopy={(code) => copyToClipboard(code, 'settings.yaml')}
                                        maxHeight={220}
                                        minHeight={180}
                                    />
                                )}
                                {settingsTab === 'windows' && (
                                    <CodeBlock
                                        code={windowsSettingsScript}
                                        language="js"
                                        filename="PowerShell script to setup settings.yaml"
                                        wrap={true}
                                        onCopy={(code) => copyToClipboard(code, 'Windows settings script')}
                                        maxHeight={260}
                                        minHeight={220}
                                    />
                                )}
                                {settingsTab === 'unix' && (
                                    <CodeBlock
                                        code={unixSettingsScript}
                                        language="js"
                                        filename="Bash script to setup settings.yaml"
                                        wrap={true}
                                        onCopy={(code) => copyToClipboard(code, 'Unix settings script')}
                                        maxHeight={260}
                                        minHeight={220}
                                    />
                                )}
                            </Box>
                        </Box>

                        <Box sx={{ display: 'flex', flexDirection: 'column' }}>
                            <Box sx={{ mb: 1, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                                <Typography variant="subtitle2" sx={{ color: 'text.secondary' }}>
                                    Step 2 · Create or update `$DSH_HOME/.credentials.yaml`
                                </Typography>
                                <Tabs
                                    value={credsTab}
                                    onChange={(_, value) => setCredsTab(value)}
                                    variant="standard"
                                    sx={{ minHeight: 32, '& .MuiTabs-indicator': { height: 3 } }}
                                >
                                    <Tab label="YAML" value="yaml" sx={{ minHeight: 32, py: 0.5, fontSize: '0.875rem' }} />
                                    <Tab label="Windows" value="windows" sx={{ minHeight: 32, py: 0.5, fontSize: '0.875rem' }} />
                                    <Tab label="Linux/macOS" value="unix" sx={{ minHeight: 32, py: 0.5, fontSize: '0.875rem' }} />
                                </Tabs>
                            </Box>
                            <Box sx={{ mb: 1.5 }}>
                                <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                                    Set `{DSH_API_KEY_ENV}` in `$DSH_HOME/.credentials.yaml` to the API key generated by Tingly Box. If the file already exists, update the existing value.
                                </Typography>
                            </Box>
                            <Box>
                                {credsTab === 'yaml' && (
                                    <CodeBlock
                                        code={credentialsYaml}
                                        language="yaml"
                                        filename="Create or update $DSH_HOME/.credentials.yaml"
                                        wrap={true}
                                        onCopy={(code) => copyToClipboard(code, '.credentials.yaml')}
                                        maxHeight={140}
                                        minHeight={100}
                                    />
                                )}
                                {credsTab === 'windows' && (
                                    <CodeBlock
                                        code={windowsCredsScript}
                                        language="js"
                                        filename="PowerShell script to setup .credentials.yaml"
                                        wrap={true}
                                        onCopy={(code) => copyToClipboard(code, 'Windows credentials script')}
                                        maxHeight={220}
                                        minHeight={180}
                                    />
                                )}
                                {credsTab === 'unix' && (
                                    <CodeBlock
                                        code={unixCredsScript}
                                        language="js"
                                        filename="Bash script to setup .credentials.yaml"
                                        wrap={true}
                                        onCopy={(code) => copyToClipboard(code, 'Unix credentials script')}
                                        maxHeight={220}
                                        minHeight={180}
                                    />
                                )}
                            </Box>
                        </Box>

                        {previewModels.length > 0 && (
                            <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                                {t('dshConfig.modelsPreviewNote', { models: previewModels.join(', ') })}
                            </Typography>
                        )}
                    </Box>
                )}
            </DialogContent>
            <DialogActions sx={{ px: 3, pb: 2 }}>
                <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 1, width: '100%' }}>
                    <Button onClick={onClose} variant="outlined">
                        {t('common.close')}
                    </Button>
                    <Button
                        onClick={handleApplyConfiguration}
                        variant="contained"
                        disabled={isApplying}
                        startIcon={isApplying ? <CircularProgress size={16} color="inherit" /> : null}
                    >
                        {isApplying ? t('common.applying') : t('scenarioPage.autoConfig')}
                    </Button>
                </Box>
            </DialogActions>
        </Dialog>
    );
};

export default DshConfigModal;
