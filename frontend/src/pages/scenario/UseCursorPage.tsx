import CardGrid from "@/components/CardGrid.tsx";
import UnifiedCard from "@/components/UnifiedCard.tsx";
import ProviderConfigCard from "@/components/ProviderConfigCard.tsx";
import { Box, Button, Tooltip, IconButton } from '@mui/material';
import { Info as InfoIcon } from '@/components/icons';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import PageLayout from '@/components/PageLayout';
import ScenarioPageSkeleton from './components/ScenarioPageSkeleton';
import TemplatePage from './components/TemplatePage.tsx';
import CursorConfigModal from './components/CursorConfigModal';
import { useScenarioPageInternal } from '@/pages/scenario/hooks/useScenarioPageInternal.ts';
import { ScenarioPageModalProvider } from '@/pages/scenario/context/ScenarioPageContext';
const scenario = "cursor";
const UseCursorPageContent: React.FC = () => {
    const { t } = useTranslation();
    const {
        isLoading,
        notification,
        copyToClipboard,
        baseUrl,
    } = useScenarioPageInternal(scenario);
    const [configModalOpen, setConfigModalOpen] = useState(false);
    const handleOpenConfigModal = () => {
        setConfigModalOpen(true);
    };
    return (
        <PageLayout loading={isLoading} loadingContent={<ScenarioPageSkeleton />} notification={notification}>
            <CardGrid>
                <UnifiedCard
                    titleHeadingLevel={1}
                    title={
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <span>Cursor</span>
                            <Tooltip title={t('scenarioPage.tooltip.cursor')}>
                                <IconButton size="small" sx={{ ml: 0.5 }}>
                                    <InfoIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                                </IconButton>
                            </Tooltip>
                        </Box>
                    }
                    size="full"
                    rightAction={
                        <Button
                            onClick={handleOpenConfigModal}
                            variant="contained"
                            size="small"
                        >
                            {t('scenarioPage.config')}
                        </Button>
                    }
                >
                    <ProviderConfigCard
                        title="Cursor"
                        baseUrlPath="/tingly/cursor"
                        baseUrl={baseUrl}
                        onCopy={copyToClipboard}
                        scenario={scenario}
                        showApiKeyRow={true}
                        showBaseUrlRow={true}
                        compact={true}
                    />
                </UnifiedCard>
                <TemplatePage
                    scenario={scenario}
                    collapsible={true}
                    allowDeleteRule={true}
                />
                <CursorConfigModal
                    open={configModalOpen}
                    onClose={() => setConfigModalOpen(false)}
                    baseUrl={baseUrl}
                    copyToClipboard={copyToClipboard}
                />
            </CardGrid>
        </PageLayout>
    );
};
const UseCursorPage: React.FC = () => {
    return (
        <ScenarioPageModalProvider>
            <UseCursorPageContent />
        </ScenarioPageModalProvider>
    );
};
export default UseCursorPage;
