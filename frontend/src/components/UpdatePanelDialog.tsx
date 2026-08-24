import { ContentCopy, Check, GitHub, AppRegistration as NPM, Refresh } from '@/components/icons';
import { Box, Button, Collapse, Dialog, DialogActions, DialogContent, Divider, IconButton, Stack, Typography, useTheme } from '@mui/material';
import { alpha } from '@mui/material/styles';
import { fontMono } from '@/theme/fonts';
import React, { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { TransitionGroup } from 'react-transition-group';
import { useVersion } from '@/contexts/VersionContext';
import { Paper, Tooltip } from '@mui/material';

interface UpdatePanelDialogProps {
    open: boolean;
    onClose: () => void;
}

/**
 * UpdatePanelDialog Component
 *
 * Comprehensive dialog for version checking and update instructions.
 * Displays current vs latest version, manual check button, and multiple
 * installation methods with copy-to-clipboard functionality.
 */
export const UpdatePanelDialog: React.FC<UpdatePanelDialogProps> = ({ open, onClose }) => {
    const { t } = useTranslation();
    const theme = useTheme();
    const { currentVersion, latestVersion, checking, releaseURL, checkForUpdates, hasUpdate, canOneClick, updating, updateError, applyUpdate } = useVersion();

    const [copiedIndex, setCopiedIndex] = useState<number | null>(null);
    const [copiedVersion, setCopiedVersion] = useState(false);

    const displayCurrentVersion = (currentVersion || 'Unknown').split('+')[0];
    const displayLatestVersion = (latestVersion || currentVersion || 'Unknown').split('+')[0];

    // Use backend's has_update for accurate version comparison
    const hasVersionUpdate = hasUpdate && latestVersion && currentVersion;

    // Determine which version to use for commands
    // If update is available, use latest version
    // If up to date, use latest version (not current_version which might be "dev" in dev mode)
    // Only fallback to currentVersion if latestVersion is not available
    const versionForCommand = latestVersion || currentVersion;

    // Update methods with commands - always use specific version
    const updateMethods = [
        {
            id: 'npx',
            title: t('update.methods.npx.title'),
            description: t('update.methods.npx.description'),
            command: versionForCommand ? `npx tingly-box@${versionForCommand}` : 'npx tingly-box@latest',
            icon: <NPM />,
        },
        {
            id: 'bundle',
            title: t('update.methods.bundle.title'),
            description: t('update.methods.bundle.description'),
            command: versionForCommand ? `npx -y tingly-box-bundle@${versionForCommand}` : 'npx -y tingly-box-bundle@latest',
            icon: <NPM />,
        },
        {
            id: 'docker',
            title: t('update.methods.docker.title'),
            description: t('update.methods.docker.description'),
            command: versionForCommand ? `docker pull ghcr.io/tingly-dev/tingly-box:v${versionForCommand}` : 'docker pull ghcr.io/tingly-dev/tingly-box:latest',
            icon: <GitHub />,
        },
    ] as const;

    const handleCopy = useCallback((command: string, index: number) => {
        navigator.clipboard.writeText(command).then(() => {
            setCopiedIndex(index);
            setTimeout(() => setCopiedIndex(null), 2000);
        });
    }, []);

    const handleCopyVersion = useCallback(() => {
        navigator.clipboard.writeText(displayCurrentVersion).then(() => {
            setCopiedVersion(true);
            setTimeout(() => setCopiedVersion(false), 2000);
        });
    }, [displayCurrentVersion]);

    const handleCheckForUpdates = useCallback(() => {
        checkForUpdates(true);
    }, [checkForUpdates]);

    const getStatusColor = () => {
        if (checking) return 'info';
        if (hasVersionUpdate) return 'warning';
        return 'success';
    };

    const statusColor = getStatusColor();
    const statusPalette = theme.palette[statusColor];

    const getStatusIcon = () => {
        if (checking) {
            return <Refresh sx={{ fontSize: 32, color: statusPalette.main, animation: 'spin 1s linear infinite' }} />;
        }
        if (hasVersionUpdate) {
            return <GitHub sx={{ fontSize: 32, color: statusPalette.main }} />;
        }
        return <Refresh sx={{ fontSize: 32, color: statusPalette.main }} />;
    };

    const getStatusTitle = () => {
        if (checking) {
            return t('update.checking');
        }
        if (hasVersionUpdate) {
            return t('update.updateAvailable');
        }
        return t('update.upToDate');
    };

    return (
        <Dialog
            open={open}
            onClose={onClose}
            maxWidth="sm"
            fullWidth
            slotProps={{
                paper: {
                    sx: {
                        borderRadius: 2,
                        overflow: 'hidden',
                        border: '1px solid',
                        borderColor: 'divider',
                    },
                }
            }}
        >
            {/* Header — soft tinted surface derived from the status palette color */}
            <Box
                sx={{
                    bgcolor: alpha(statusPalette.main, theme.palette.mode === 'dark' ? 0.16 : 0.10),
                    borderBottom: '1px solid',
                    borderColor: alpha(statusPalette.main, 0.24),
                    px: 3,
                    py: 2.5,
                    textAlign: 'center',
                }}
            >
                <Box
                    sx={{
                        width: 56,
                        height: 56,
                        borderRadius: '50%',
                        bgcolor: alpha(statusPalette.main, theme.palette.mode === 'dark' ? 0.24 : 0.16),
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        mx: 'auto',
                        mb: 1.5,
                    }}
                >
                    {getStatusIcon()}
                </Box>
                <Typography variant="h6" sx={{ color: 'text.primary', fontWeight: 600, mb: 0.5 }}>
                    {getStatusTitle()}
                </Typography>
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 0.5 }}>
                    <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                        {hasVersionUpdate ? (
                            t('update.versionComparison', {
                                latest: displayLatestVersion,
                                current: displayCurrentVersion,
                            })
                        ) : (
                            t('update.currentVersion', { version: displayCurrentVersion })
                        )}
                    </Typography>
                    <Tooltip title={copiedVersion ? t('update.copied') : t('update.copy')} placement="top" arrow>
                        <IconButton
                            size="small"
                            onClick={handleCopyVersion}
                            aria-label={t('update.copy')}
                            sx={{ color: copiedVersion ? 'success.main' : 'text.secondary' }}
                        >
                            {copiedVersion ? <Check sx={{ fontSize: 16 }} /> : <ContentCopy sx={{ fontSize: 16 }} />}
                        </IconButton>
                    </Tooltip>
                </Box>
            </Box>
            <DialogContent sx={{ p: 0 }}>
                <Stack spacing={0} divider={<Divider />}>
                    {/* Manual Check Section */}
                    <Box sx={{ p: 2.5 }}>
                        <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1.5, color: 'text.primary' }}>
                            {t('update.checkUpdates')}
                        </Typography>
                        <Button
                            variant={hasVersionUpdate ? 'contained' : 'outlined'}
                            color={hasVersionUpdate ? 'warning' : 'primary'}
                            onClick={handleCheckForUpdates}
                            disabled={checking}
                            startIcon={checking ? <Refresh sx={{ fontSize: 18, animation: 'spin 1s linear infinite' }} /> : <Refresh />}
                            fullWidth
                            sx={{ height: 48 }}
                        >
                            {checking ? t('update.checking') : t('update.check')}
                        </Button>
                    </Box>

                    {/* One-Click Update Section — npx installs only: the server
                        relaunches itself through npx pinned to the new version
                        and repins existing shortcuts. */}
                    {hasVersionUpdate && canOneClick && (
                        <Box sx={{ p: 2.5 }}>
                            <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 0.5, color: 'text.primary' }}>
                                {t('update.oneClick.title')}
                            </Typography>
                            <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mb: 1.5 }}>
                                {t('update.oneClick.hint', { version: displayLatestVersion })}
                            </Typography>
                            <Button
                                variant="contained"
                                color="primary"
                                onClick={applyUpdate}
                                disabled={updating}
                                startIcon={updating ? <Refresh sx={{ fontSize: 18, animation: 'spin 1s linear infinite' }} /> : <NPM />}
                                fullWidth
                                sx={{ height: 48 }}
                            >
                                {updating ? t('update.oneClick.updating') : t('update.oneClick.button', { version: displayLatestVersion })}
                            </Button>
                            {updating && (
                                <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mt: 1 }}>
                                    {t('update.oneClick.inProgress', { version: displayLatestVersion })}
                                </Typography>
                            )}
                            {updateError && (
                                <Typography variant="caption" sx={{ color: 'error.main', display: 'block', mt: 1 }}>
                                    {updateError === 'timeout'
                                        ? t('update.oneClick.timeout')
                                        : t('update.oneClick.failed', { error: updateError })}
                                </Typography>
                            )}
                        </Box>
                    )}

                    {/* Update Methods Section */}
                    <Box sx={{ p: 2.5 }}>
                        <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1.5, color: 'text.primary' }}>
                            {t('update.updateMethods')}
                        </Typography>

                        <TransitionGroup>
                            {updateMethods.map((method, index) => (
                                <Collapse key={method.id}>
                                    <Box sx={{ mb: index < updateMethods.length - 1 ? 2 : 0 }}>
                                        <Typography
                                            variant="body2"
                                            sx={{
                                                fontWeight: 500,
                                                mb: 0.5,
                                                color: 'text.primary',
                                                display: 'flex',
                                                alignItems: 'center',
                                                gap: 0.5,
                                            }}
                                        >
                                            {method.icon}
                                            {method.title}
                                        </Typography>
                                        <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mb: 1 }}>
                                            {method.description}
                                        </Typography>
                                        <Paper
                                            variant="outlined"
                                            sx={{
                                                p: 2,
                                                bgcolor: 'background.paper',
                                                border: '1px solid',
                                                borderColor: 'divider',
                                                position: 'relative',
                                            }}
                                        >
                                            <Typography
                                                variant="body2"
                                                sx={{
                                                    fontFamily: fontMono,
                                                    color: 'text.primary',
                                                    fontSize: '0.875rem',
                                                    pr: 5,
                                                    wordBreak: 'break-all',
                                                }}
                                            >
                                                $ {method.command}
                                            </Typography>
                                            <Tooltip
                                                title={copiedIndex === index ? t('update.copied') : t('update.copy')}
                                                placement="top"
                                                arrow
                                            >
                                                <IconButton
                                                    size="small"
                                                    onClick={() => handleCopy(method.command, index)}
                                                    sx={{
                                                        position: 'absolute',
                                                        right: 8,
                                                        top: '50%',
                                                        transform: 'translateY(-50%)',
                                                        color: copiedIndex === index ? 'success.main' : 'text.secondary',
                                                        bgcolor: copiedIndex === index ? 'success.light' : 'transparent',
                                                        '&:hover': {
                                                            color: 'primary.main',
                                                            bgcolor: 'action.hover',
                                                        },
                                                    }}
                                                >
                                                    <ContentCopy sx={{ fontSize: 18 }} />
                                                </IconButton>
                                            </Tooltip>
                                        </Paper>
                                    </Box>
                                </Collapse>
                            ))}
                        </TransitionGroup>
                    </Box>
                </Stack>
            </DialogContent>
            <DialogActions sx={{ px: 3, py: 2, bgcolor: 'action.hover', justifyContent: 'space-between' }}>
                <Button
                    onClick={() => window.open(releaseURL || 'https://github.com/tingly-dev/tingly-box/releases', '_blank')}
                    startIcon={<GitHub />}
                    sx={{
                        color: 'text.secondary',
                        '&:hover': {
                            bgcolor: 'action.selected',
                        },
                    }}
                >
                    {t('update.releaseNotes')}
                </Button>
                <Button
                    onClick={onClose}
                    sx={{
                        color: 'text.secondary',
                        '&:hover': {
                            bgcolor: 'action.selected',
                        },
                    }}
                >
                    {t('common.close')}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default UpdatePanelDialog;
