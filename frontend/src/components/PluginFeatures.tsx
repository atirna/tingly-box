import { Box, CircularProgress, Typography } from '@mui/material';
import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import { ConfigRow } from './ConfigRow';
import {
    PluginToggleButton,
    RecordingV2Control,
    ThinkingEffortControl,
    VisionProxyControl,
    WebProxyControl,
} from './flags';
import type { ServiceRef } from './flags';
import type { Provider } from '@/types/provider';

export interface PluginFeaturesProps {
    scenario: string;
}

interface PluginFeatureConfig {
    key: string;
    label: string;
    description: string;
    scenarios?: readonly string[];
}

// Scenario-level boolean plugins. Only flags that genuinely have a
// scenario-level default belong here. `clean_header` was deliberately
// dropped: it is now rule-only (backend `SetScenarioFlag` rejects it as an
// unknown flag, so the toggle could never persist) — it lives on the per-rule
// Plugins card instead. See .design/rule-flags.md §4 / §12.
const PLUGIN_FEATURES: PluginFeatureConfig[] = [
    { key: 'smart_compact', label: 'Smart Compact', description: 'Remove thinking blocks from conversation history to reduce context' },
];

// Scenario-level service_ref plugins. These live in ScenarioConfig.Extensions
// (not ScenarioFlags): a flat bool/string field cannot hold a {provider, model}
// pair. Both are "configured ⇒ enabled" — there is no separate on/off flag, so
// clearing the service is how you turn the feature off.
const VISION_PROXY_SERVICE_KEY = 'vision_proxy_service';
const WEB_PROXY_SERVICE_KEY = 'web_proxy_service';

// Endpoints that don't speak the chat/completion shape. Thinking effort,
// Smart Compact (conversation-history pruning) and the Vision / Web proxies
// have no meaning for an embedding or image-generation endpoint, so we hide
// them there instead
// of showing dead controls. Kept as a blacklist so any new *chat* scenario
// automatically inherits the full plugin set. See UX principle #9 (reduce
// visual noise) / #1 (organize around the user's real question).
const NON_CHAT_SCENARIOS = new Set(['embed', 'imagegen']);

// A half-filled pair is not a configuration — same rule the backend's
// IsActive() applies — so it reads back as "off" rather than as a broken
// half-state the UI would have to explain.
const readServiceRef = (
    extensions: Record<string, any> | undefined,
    key: string,
): ServiceRef | null => {
    const svc = extensions?.[key];
    return svc?.provider && svc?.model ? { provider: svc.provider, model: svc.model } : null;
};

const PluginFeatures: React.FC<PluginFeaturesProps> = ({ scenario }) => {
    const baseScenario = scenario.includes(':') ? scenario.split(':')[0] : scenario;
    const isChatShaped = !NON_CHAT_SCENARIOS.has(baseScenario);

    const [features, setFeatures] = useState<Record<string, boolean>>({});
    const [effort, setEffort] = useState<string>('');
    const [recordV2Mode, setRecordV2Mode] = useState<string>('');
    const [loading, setLoading] = useState(true);
    const [updating, setUpdating] = useState<Record<string, boolean>>({});

    const [visionService, setVisionService] = useState<ServiceRef | null>(null);
    const [webService, setWebService] = useState<ServiceRef | null>(null);
    const [providers, setProviders] = useState<Provider[]>([]);

    // Smart Compact only applies to chat-shaped endpoints; drop it (and any
    // other conversation-oriented boolean plugin) for embedding / image gen.
    const visibleFeatures = isChatShaped
        ? PLUGIN_FEATURES.filter(f => !f.scenarios || f.scenarios.includes(baseScenario as any))
        : [];

    const loadData = async () => {
        try {
            setLoading(true);

            const [effortResult, recordV2Result, cfgResult, providersResult, ...featureResults] =
                await Promise.all([
                    api.getScenarioStringFlag(scenario, 'thinking_effort'),
                    api.getScenarioStringFlag(scenario, 'recording_v2'),
                    api.getScenarioConfig(scenario),
                    api.getProviders(),
                    ...visibleFeatures.map(f => api.getScenarioFlag(scenario, f.key)),
                ]);

            if (effortResult?.success && effortResult?.data?.value !== undefined) {
                setEffort(effortResult.data.value);
            }
            if (recordV2Result?.success && recordV2Result?.data?.value !== undefined) {
                setRecordV2Mode(recordV2Result.data.value);
            }

            const ext = cfgResult?.data?.extensions || cfgResult?.data?.Extensions;
            setVisionService(readServiceRef(ext, VISION_PROXY_SERVICE_KEY));
            setWebService(readServiceRef(ext, WEB_PROXY_SERVICE_KEY));

            if (providersResult?.success && Array.isArray(providersResult.data)) {
                setProviders(providersResult.data);
            }

            const newFeatures: Record<string, boolean> = {};
            visibleFeatures.forEach((f, i) => {
                newFeatures[f.key] = featureResults[i]?.success && featureResults[i]?.data?.value !== undefined
                    ? featureResults[i].data.value
                    : false;
            });
            setFeatures(newFeatures);
        } catch (error) {
            console.error('Failed to load scenario features:', error);
        } finally {
            setLoading(false);
        }
    };

    const setFeature = (featureKey: string, value: boolean) => {
        if (updating[featureKey]) return;
        setUpdating(prev => ({ ...prev, [featureKey]: true }));
        api.setScenarioFlag(scenario, featureKey, value)
            .then(result => result.success ? setFeatures(prev => ({ ...prev, [featureKey]: value })) : loadData())
            .catch(() => loadData())
            .finally(() => setUpdating(prev => ({ ...prev, [featureKey]: false })));
    };

    // Scenario string-flags share one optimistic save flow; build the setters
    // from a single factory keyed by flag name (also the in-flight key).
    const makeStringFlagSetter = (
        flagKey: string,
        current: string,
        setLocal: (value: string) => void,
    ) => (next: string) => {
        if (updating[flagKey] || next === current) return;
        setUpdating(prev => ({ ...prev, [flagKey]: true }));
        api.setScenarioStringFlag(scenario, flagKey, next)
            .then(result => (result.success ? setLocal(next) : loadData()))
            .catch(() => loadData())
            .finally(() => setUpdating(prev => ({ ...prev, [flagKey]: false })));
    };

    const setEffortLevel = makeStringFlagSetter('thinking_effort', effort, setEffort);
    const setRecordV2 = makeStringFlagSetter('recording_v2', recordV2Mode, setRecordV2Mode);

    // Both service_ref plugins save the same way, so they share one flow.
    //
    // The GET-merge is load-bearing, not defensive: the backend's
    // SetScenarioConfig replaces the scenario wholesale, so POSTing a partial
    // config silently wipes every other extension — including the sibling
    // proxy's service. See .design/vision-proxy.md §6.2.
    const makeServiceRefSetter = (
        extensionKey: string,
        setLocal: (value: ServiceRef | null) => void,
    ) => async (next: ServiceRef | null) => {
        setUpdating(prev => ({ ...prev, [extensionKey]: true }));
        try {
            const cfgResult = await api.getScenarioConfig(scenario);
            const cfg = cfgResult?.data || {};
            const extensions = { ...(cfg.extensions || cfg.Extensions || {}) };
            if (next) {
                extensions[extensionKey] = next;
            } else {
                delete extensions[extensionKey];
            }
            const result = await api.setScenarioConfig(scenario, { ...cfg, scenario, extensions });
            if (result?.success) {
                setLocal(next);
            } else {
                loadData();
            }
        } catch {
            loadData();
        } finally {
            setUpdating(prev => ({ ...prev, [extensionKey]: false }));
        }
    };

    const handleVisionChange = makeServiceRefSetter(VISION_PROXY_SERVICE_KEY, setVisionService);
    const handleWebChange = makeServiceRefSetter(WEB_PROXY_SERVICE_KEY, setWebService);

    useEffect(() => {
        loadData();
    }, [scenario]);

    if (loading) {
        return (
            <Box sx={{ display: 'flex', flexDirection: 'column', py: 2, gap: 2, alignItems: 'center', justifyContent: 'center', minHeight: 100 }}>
                <CircularProgress size={24} />
                <Typography variant="body2" sx={{
                    color: "text.secondary"
                }}>Loading features...</Typography>
            </Box>
        );
    }

    return (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <ConfigRow
                tabs={[
                    {
                        key: 'plugins',
                        label: 'Plugins',
                        content: (
                            <Box sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', columnGap: 1.5, rowGap: 1, width: '100%' }}>
                                {isChatShaped && (
                                    <ThinkingEffortControl
                                        value={effort}
                                        disabled={updating.thinking_effort}
                                        onChange={setEffortLevel}
                                    />
                                )}
                                {visibleFeatures.map(feature => (
                                    <PluginToggleButton
                                        key={feature.key}
                                        label={feature.label}
                                        description={feature.description}
                                        value={features[feature.key] || false}
                                        disabled={updating[feature.key] || false}
                                        onChange={v => setFeature(feature.key, v)}
                                    />
                                ))}
                                {isChatShaped && (
                                    <VisionProxyControl
                                        value={visionService}
                                        providers={providers}
                                        disabled={updating.vision_proxy_service || false}
                                        onChange={handleVisionChange}
                                    />
                                )}
                                {isChatShaped && (
                                    <WebProxyControl
                                        value={webService}
                                        providers={providers}
                                        disabled={updating.web_proxy_service || false}
                                        onChange={handleWebChange}
                                    />
                                )}
                                <RecordingV2Control
                                    value={recordV2Mode}
                                    disabled={updating.recording_v2 || false}
                                    onChange={setRecordV2}
                                />
                            </Box>
                        ),
                    },
                ]}
                activeTab="plugins"
                onTabChange={() => {}}
                maxWidth="responsive"
            />
        </Box>
    );
};

export default PluginFeatures;
