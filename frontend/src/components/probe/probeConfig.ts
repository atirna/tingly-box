import type { ProbeThinking, ProbeProtocol, ProbeResult, ProbeTargetType, ProbeTestMode } from '@/types/probe';
import type { Provider } from '@/types/provider';

// ProbeAxes is the panel's orthogonal control state. Every axis is one knob:
// Shape (stream) × Tool × Thinking × Protocol × Scope (direct) — plus the
// optional message override. Persisted per target-type so the dialog re-opens
// in the shape the user last used on that surface.
export interface ProbeAxes {
    stream: boolean;
    tool: boolean;
    thinking: ProbeThinking;
    // '' means "no protocol override" — the backend resolves the target's
    // primary protocol (provider APIStyle, Codex OAuth → Responses).
    protocol: ProbeProtocol | '';
    direct: boolean;
}

export const DEFAULT_AXES: ProbeAxes = {
    stream: true, // Stream default — closest to production traffic
    tool: false,
    thinking: 'none',
    protocol: '',
    direct: false,
};

const STORAGE_PREFIX = 'tb.probe.config.';

// loadPersistedAxes restores the last-used axes for a target-type surface.
// Anything malformed (or from an older shape) is ignored.
export function loadPersistedAxes(targetType: ProbeTargetType): Partial<ProbeAxes> | null {
    try {
        const raw = localStorage.getItem(STORAGE_PREFIX + targetType);
        if (!raw) return null;
        const parsed = JSON.parse(raw);
        if (typeof parsed !== 'object' || parsed === null) return null;
        return parsed as Partial<ProbeAxes>;
    } catch {
        return null;
    }
}

export function persistAxes(targetType: ProbeTargetType, axes: ProbeAxes) {
    try {
        localStorage.setItem(STORAGE_PREFIX + targetType, JSON.stringify(axes));
    } catch {
        // localStorage unavailable (private mode etc.) — persistence is best-effort.
    }
}

// legacyModeToAxes maps the old test_mode prop spelling onto the axes. Tool
// mode historically takes the non-stream path (structured tool_calls).
function legacyModeToAxes(mode: ProbeTestMode): Partial<ProbeAxes> {
    switch (mode) {
        case 'streaming':
            return { stream: true, tool: false };
        case 'tool':
            return { stream: false, tool: true };
        case 'simple':
            return { stream: false, tool: false };
    }
}

// resolveInitialAxes applies the open-time association priority:
//   1. explicit prop overrides (legacy testMode/thinkingLevel props)
//   2. the pre-computed initialResult — the visible state must match the
//      result the user is looking at (shape from result.data.stream)
//   3. last-used axes persisted for this target-type surface
//   4. defaults (Stream / no tool / no thinking / provider's primary protocol / Through TB)
export function resolveInitialAxes(opts: {
    targetType: ProbeTargetType;
    testMode?: ProbeTestMode;
    thinkingLevel?: ProbeThinking;
    initialResult?: ProbeResult;
    provider?: Provider | null;
}): ProbeAxes {
    const axes: ProbeAxes = { ...DEFAULT_AXES };

    // Priority 3: persisted last-used config.
    Object.assign(axes, loadPersistedAxes(opts.targetType));

    // Priority 2: a pre-loaded result wins — the toggles must describe the
    // request that produced it.
    if (opts.initialResult?.data && typeof opts.initialResult.data.stream === 'boolean') {
        axes.stream = opts.initialResult.data.stream;
    }

    // Priority 1: explicit props (kept for callers that know better).
    if (opts.testMode) Object.assign(axes, legacyModeToAxes(opts.testMode));
    if (opts.thinkingLevel) axes.thinking = opts.thinkingLevel;

    // Protocol/scope availability clamp (e.g. '' protocol for google targets
    // is fine, but a persisted anthropic protocol must not stick onto a
    // provider that can't speak it).
    const avail = protocolAvailability(opts.provider ?? null);
    if (avail.locked) {
        axes.protocol = avail.default;
    } else if (axes.protocol && !avail.options.includes(axes.protocol)) {
        axes.protocol = '';
    }
    if (!scopeAvailable(opts.targetType)) {
        axes.direct = false;
    }

    return axes;
}

// scopeAvailable: only provider targets can bypass TB. Rule probes must
// traverse the middleware they exist to test.
export function scopeAvailable(targetType: ProbeTargetType): boolean {
    return targetType === 'provider';
}

export interface ProtocolAvailability {
    // Options offered on the Protocol axis, in display order.
    options: ProbeProtocol[];
    // The target's primary protocol (the smart default).
    default: ProbeProtocol | '';
    // Locked means the target speaks exactly one protocol — render the axis
    // disabled with that value. Disabled (options empty + locked with '')
    // means no protocol axis at all (Google's own SDK).
    locked: boolean;
}

// protocolAvailability reduces the Protocol axis per target. Brand-first
// labels (OpenAI Chat / OpenAI Responses / Anthropic) live in i18n; bare
// "Responses"/"Messages" assume SDK knowledge users don't have.
export function protocolAvailability(provider: Provider | null): ProtocolAvailability {
    if (!provider) {
        // Unknown target (still loading) — offer nothing, don't lock.
        return { options: [], default: '', locked: false };
    }
    if (provider.api_style === 'google') {
        return { options: [], default: '', locked: true };
    }
    const isCodexOAuth = provider.oauth_detail?.issuer === 'codex';
    if (provider.api_style === 'anthropic') {
        return { options: ['anthropic_v1'], default: 'anthropic_v1', locked: true };
    }
    const hasDualAnthropic = !!provider.api_base_anthropic;
    const options: ProbeProtocol[] = ['openai_chat', 'openai_responses'];
    if (hasDualAnthropic) options.push('anthropic_v1');
    const primary: ProbeProtocol = isCodexOAuth ? 'openai_responses' : 'openai_chat';
    return { options, default: primary, locked: false };
}
