import CardGrid from "@/components/CardGrid.tsx";
import UnifiedCard from "@/components/UnifiedCard.tsx";
import ProviderConfigCard from "@/components/ProviderConfigCard.tsx";
import AgentSetupCard, { type AgentApplyResult, hasModelOnAnyRule, scrollToModelsCard } from './components/AgentSetupCard';
import ConnectAIDialogs from '@/components/ConnectAIDialogs';
import {useProviderDialog} from '@/hooks/useProviderDialog';
import { defaultDshPrefs } from './components/DshQuickConfig';
import { api } from '@/services/api';
import { Box, Button, Tooltip, IconButton } from '@mui/material';
import { Info as InfoIcon } from '@/components/icons';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import PageLayout from '@/components/PageLayout';
import ScenarioPageSkeleton from './components/ScenarioPageSkeleton';
import TemplatePage from './components/TemplatePage.tsx';
import DshConfigModal from './components/DshConfigModal';
import { useScenarioPageInternal } from '@/pages/scenario/hooks/useScenarioPageInternal.ts';
import { ScenarioPageModalProvider } from '@/pages/scenario/context/ScenarioPageContext';
const scenario = "dsh";
const DSH_REPO_URL = 'https://github.com/deepseek-ai/deepseek-harness';
// dsh serves a local Web UI; tingly-box does not launch it (security), the
// frontend only offers a jump to the default address.
const DSH_WEB_UI_URL = 'http://127.0.0.1:3080';
const UseDshPageContent: React.FC = () => {
    const { t } = useTranslation();
    const {
        isLoading,
        notification,
        showNotification,
        copyToClipboard,
        baseUrl,
        rules,
    } = useScenarioPageInternal(scenario);
    const [configModalOpen, setConfigModalOpen] = useState(false);
    const [isApplyLoading, setIsApplyLoading] = useState(false);
    // Unified Connect AI add flow (picker + form/OAuth/paste/import dialogs).
    const connectAI = useProviderDialog(showNotification, {
        onProviderAdded: () => window.location.reload(),
    });
    const handleOpenConfigModal = () => {
        setConfigModalOpen(true);
    };
    const handleApply = async (): Promise<AgentApplyResult> => {
        try {
            setIsApplyLoading(true);
            const result = await api.applyDshConfig(defaultDshPrefs() as Record<string, string>);
            if (result.success) {
                const files: string[] = [];
                if (result.settingsResult?.created || result.settingsResult?.updated) {
                    files.push('$DSH_HOME/settings.yaml');
                }
                if (result.credentialsResult?.created || result.credentialsResult?.updated) {
                    files.push('$DSH_HOME/.credentials.yaml');
                }
                return { success: true, files };
            }
            return { success: false, error: result.message || t('scenarioPage.unknownError') };
        } catch (err: any) {
            return { success: false, error: err?.message || t('dshConfig.applyFailed') };
        } finally {
            setIsApplyLoading(false);
        }
    };
    return (
        <PageLayout loading={isLoading} loadingContent={<ScenarioPageSkeleton />} notification={notification}>
            <CardGrid>
                <UnifiedCard
                    titleHeadingLevel={1}
                    title={
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <span>DeepSeek Harness</span>
                            <Tooltip title={t('scenarioPage.tooltip.dsh')}>
                                <IconButton size="small" sx={{ ml: 0.5 }}>
                                    <InfoIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                                </IconButton>
                            </Tooltip>
                        </Box>
                    }
                    size="full"
                    rightAction={
                        <Box sx={{ display: 'flex', gap: 1 }}>
                            <Tooltip title={DSH_WEB_UI_URL}>
                                <Button
                                    href={DSH_WEB_UI_URL}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    variant="contained"
                                    size="small"
                                >
                                    {t('scenarioPage.dsh.openWebUi')}
                                </Button>
                            </Tooltip>
                            <Button
                                onClick={handleOpenConfigModal}
                                variant="outlined"
                                size="small"
                            >
                                {t('scenarioPage.autoConfig')}
                            </Button>
                        </Box>
                    }
                >
                    <ProviderConfigCard
                        title="DeepSeek Harness"
                        baseUrlPath="/tingly/dsh"
                        baseUrl={baseUrl}
                        onCopy={copyToClipboard}
                        scenario={scenario}
                        showApiKeyRow={true}
                        compact={true}
                    />
                </UnifiedCard>
                <AgentSetupCard
                    agentKey={scenario}
                    agentName="DeepSeek Harness"
                    installCommand="npx @deepseek-ai/dsh web"
                    installStepDescription={t('scenarioPage.dsh.installDescription')}
                    installActions={[
                        { label: t('scenarioPage.dsh.openWebUi'), href: DSH_WEB_UI_URL, variant: 'contained', external: true },
                        { label: t('scenarioPage.dsh.viewRepo'), href: DSH_REPO_URL, variant: 'outlined', external: true },
                    ]}
                    onApply={handleApply}
                    isApplyLoading={isApplyLoading}
                    onViewConfig={handleOpenConfigModal}
                    applyStepLabel={t('scenarioPage.dsh.applyStepLabel')}
                    applyStepDescription={t('scenarioPage.dsh.applyStepDescription')}
                    viewConfigButtonLabel={t('scenarioPage.dsh.openGuide')}
                    hasModelSelected={hasModelOnAnyRule(rules)}
                    onSelectModel={scrollToModelsCard}
                    onConnectProvider={connectAI.handleConnectAIClick}
                />
                <TemplatePage
                    scenario={scenario}
                    collapsible={true}
                    allowDeleteRule={true}
                />
                <DshConfigModal
                    open={configModalOpen}
                    onClose={() => setConfigModalOpen(false)}
                    copyToClipboard={copyToClipboard}
                    showNotification={showNotification}
                />
                <ConnectAIDialogs flow={connectAI}/>
            </CardGrid>
        </PageLayout>
    );
};
const UseDshPage: React.FC = () => {
    return (
        <ScenarioPageModalProvider>
            <UseDshPageContent />
        </ScenarioPageModalProvider>
    );
};
export default UseDshPage;
