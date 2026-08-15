export type UsageMetricKey =
    | 'requests'
    | 'total'
    | 'cacheRead'
    | 'cacheWrite'
    | 'cacheHit'
    | 'input'
    | 'output'
    | 'reasoning'
    | 'errorRate';

export interface UsageMetricLabels {
    requests: string;
    total: string;
    cacheRead: string;
    cacheWrite: string;
    cacheHit: string;
    input: string;
    output: string;
    reasoning: string;
    errorRate: string;
}

export interface UsageMetricSource {
    request_count: number;
    total_input_tokens?: number;
    total_output_tokens?: number;
    cache_read_tokens?: number;
    cache_write_tokens?: number;
    reasoning_tokens?: number;
    error_rate?: number;
}

export interface UsageMetricOptions {
    showTotal?: boolean;
    showCacheWrite?: boolean;
}

export interface UsageMetricColumn {
    key: UsageMetricKey;
    label: string;
}

export const defaultUsageMetricLabels: UsageMetricLabels = {
    requests: 'Requests',
    total: 'Total',
    cacheRead: 'Cache Read',
    cacheWrite: 'Cache Write',
    cacheHit: 'Cache Hit',
    input: 'Input Tokens',
    output: 'Output Tokens',
    reasoning: 'Reasoning Tokens',
    errorRate: 'Error Rate',
};

export const getUsageMetricColumns = (
    { showTotal = false, showCacheWrite = false }: UsageMetricOptions = {},
    labels: UsageMetricLabels = defaultUsageMetricLabels,
): UsageMetricColumn[] => [
    { key: 'requests', label: labels.requests },
    ...(showTotal ? [{ key: 'total' as const, label: labels.total }] : []),
    { key: 'cacheRead', label: labels.cacheRead },
    ...(showCacheWrite ? [{ key: 'cacheWrite' as const, label: labels.cacheWrite }] : []),
    { key: 'cacheHit', label: labels.cacheHit },
    { key: 'input', label: labels.input },
    { key: 'output', label: labels.output },
    { key: 'reasoning', label: labels.reasoning },
    { key: 'errorRate', label: labels.errorRate },
];
