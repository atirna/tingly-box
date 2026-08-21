import React from 'react';
import { IconButton, Tooltip } from '@mui/material';
import { CheckCircle as CheckIcon, Error as ErrorIcon } from '@/components/icons';
import type { ProbeResult } from '@/types/probe.ts';

// ProbeDevControls: dev-only "simulate success/failure" affordance so the
// result panel can be eyeballed without a live backend. Split out of
// ProbeDialog.tsx so the shipped component reads as the shipped component,
// not shipped-plus-fixture; tree-shaken from prod builds via import.meta.env.DEV.
interface ProbeDevControlsProps {
    targetName: string;
    model?: string;
    stream: boolean;
    onSimulate: (result: ProbeResult) => void;
}

export const ProbeDevControls: React.FC<ProbeDevControlsProps> = ({ targetName, model, stream, onSimulate }) => {
    if (!import.meta.env.DEV) return null;

    return (
        <>
            <Tooltip title="Simulate Success">
                <IconButton
                    size="small"
                    onClick={() =>
                        onSimulate({
                            success: true,
                            data: {
                                content: 'Simulated success response for demo purposes',
                                latency_ms: 450,
                                request_url: 'https://api.example.com/v1/chat',
                                stream,
                                usage: {
                                    input_tokens: 25,
                                    output_tokens: 18,
                                },
                                selected_provider: targetName,
                                selected_model: model || 'claude-sonnet-4-20250514',
                                routing_source: 'smart_routing',
                                matched_smart_rule: 1,
                                upstream_api: 'anthropic_v1',
                                upstream_url: 'https://api.anthropic.com/v1/messages',
                                matched_rule: 'test-rule',
                                matched_rule_desc: 'Test Rule Description',
                                applied_flags: 'stream,bypass_cache',
                            },
                        })
                    }
                    sx={{ color: 'success.main' }}
                >
                    <CheckIcon fontSize="small" />
                </IconButton>
            </Tooltip>
            <Tooltip title="Simulate Failure">
                <IconButton
                    size="small"
                    onClick={() =>
                        onSimulate({
                            success: false,
                            error: {
                                message: 'Simulated error for demo purposes: Connection timeout',
                                type: 'upstream_error',
                            },
                        })
                    }
                    sx={{ color: 'error.main' }}
                >
                    <ErrorIcon fontSize="small" />
                </IconButton>
            </Tooltip>
        </>
    );
};

export default ProbeDevControls;
