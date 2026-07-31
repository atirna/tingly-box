// Cloud credential field schemas for the "Connect AI → Cloud" flow.
//
// The *cards* (which clouds exist, their names/icons/models) are data-driven —
// they come from the backend provider templates (providers.json, auth_type ∈
// the cloud set). Only the *field schema* and endpoint shape are code, keyed by
// auth_type, because they are types/labels/secret-flags and a URL builder, not
// data. These keys mirror the backend ai.CredentialSchema (ai/credential.go) and
// MUST match the canonical CredentialBundle keys the server validates.

export type CloudFieldType = 'text' | 'password' | 'multiline';

export interface CloudField {
    /** CredentialBundle key — must match ai/credential.go exactly. */
    key: string;
    label: string;
    type: CloudFieldType;
    required?: boolean;
    /** Rendered under an "Advanced" divider; used for optional/rare fields. */
    advanced?: boolean;
    placeholder?: string;
    helper?: string;
}

/** Auth types that use multi-field cloud credentials. */
export const CLOUD_AUTH_TYPES = ['aws_sigv4', 'gcp_sa', 'azure_key'] as const;

export function isCloudAuthType(authType?: string | null): boolean {
    return !!authType && (CLOUD_AUTH_TYPES as readonly string[]).includes(authType);
}

/**
 * Credential field schema per cloud auth type (mirror of ai.CredentialSchema).
 * AWS access key / secret / bearer are intentionally NOT flagged required: the
 * backend accepts EITHER the key pair OR a Bedrock API key (bearer_token); that
 * either/or rule lives in validateCloudFields below, matching
 * ai.ValidateCredential. All three are primary fields so both alternatives are
 * visible — only the rare session_token hides behind Advanced.
 */
export const CLOUD_FIELDS: Record<string, CloudField[]> = {
    aws_sigv4: [
        {key: 'region', label: 'Region', type: 'text', required: true, placeholder: 'us-east-1'},
        {key: 'access_key_id', label: 'Access Key ID', type: 'text', placeholder: 'AKIA…'},
        {key: 'secret_access_key', label: 'Secret Access Key', type: 'password'},
        {key: 'bearer_token', label: 'Bedrock API Key', type: 'password', helper: 'Alternative to the access key pair — provide one or the other'},
        {key: 'session_token', label: 'Session Token', type: 'password', advanced: true, helper: 'Optional — for temporary (STS) credentials'},
    ],
    gcp_sa: [
        {key: 'project_id', label: 'Project ID', type: 'text', required: true, placeholder: 'my-gcp-project'},
        {key: 'location', label: 'Location', type: 'text', required: true, placeholder: 'us-east5'},
        {key: 'service_account_json', label: 'Service Account JSON', type: 'multiline', required: true, placeholder: '{ "type": "service_account", … }'},
    ],
    azure_key: [
        {key: 'endpoint', label: 'Endpoint', type: 'text', required: true, placeholder: 'https://my-resource.openai.azure.com'},
        {key: 'api_version', label: 'API Version', type: 'text', required: true, placeholder: '2024-10-21'},
        {key: 'api_key', label: 'API Key', type: 'password', required: true},
    ],
};

export function getCloudFields(authType?: string | null): CloudField[] {
    return (authType && CLOUD_FIELDS[authType]) || [];
}

/**
 * Translator seam matching i18next's `t(key, {defaultValue, ...})` call shape,
 * so this schema module doesn't import i18next directly.
 */
export type CloudTranslate = (key: string, options: {defaultValue: string; [k: string]: unknown}) => string;

export function cloudFieldLabel(f: CloudField, t: CloudTranslate): string {
    return t(`cloudDialog.fields.${f.key}.label`, {defaultValue: f.label});
}

// Cloud region/location identifiers: lowercase alphanumerics and hyphens
// (us-east-1, us-east5, global, us, eu).
const REGION_RE = /^[a-z0-9][a-z0-9-]*$/;

/** Whether every required field of the auth type has a non-empty value. */
export function requiredCloudFieldsFilled(authType: string, v: Record<string, string>): boolean {
    return getCloudFields(authType).every((f) => !f.required || (v[f.key] || '').trim().length > 0);
}

/**
 * Validate the cross-field and format rules, mirroring ai.ValidateCredential
 * (AWS keys-OR-bearer, SA-JSON/endpoint/region format). Plain required-ness is
 * requiredCloudFieldsFilled — the form communicates it via asterisks, so it
 * produces no message here. Returns a human-readable error or null.
 */
export function validateCloudFields(
    authType: string,
    v: Record<string, string>,
    t: CloudTranslate,
): string | null {
    const get = (k: string) => (v[k] || '').trim();
    switch (authType) {
        case 'aws_sigv4': {
            const hasKeys = !!get('access_key_id') && !!get('secret_access_key');
            const hasBearer = !!get('bearer_token');
            if (!hasKeys && !hasBearer) {
                return t('cloudDialog.validation.awsEitherOr', {
                    defaultValue: 'Provide either Access Key ID + Secret Access Key, or a Bedrock API Key',
                });
            }
            if (!REGION_RE.test(get('region'))) {
                return t('cloudDialog.validation.badRegion', {
                    defaultValue: 'Region must look like "us-east-1" (lowercase letters, digits, hyphens)',
                });
            }
            break;
        }
        case 'gcp_sa': {
            if (!REGION_RE.test(get('location'))) {
                return t('cloudDialog.validation.badLocation', {
                    defaultValue: 'Location must look like "us-east5" or "global" (lowercase letters, digits, hyphens)',
                });
            }
            try {
                JSON.parse(get('service_account_json'));
            } catch {
                return t('cloudDialog.validation.badSaJson', {
                    defaultValue: 'Service Account JSON is not valid JSON — paste the full key file content',
                });
            }
            break;
        }
        case 'azure_key': {
            let ok = false;
            try {
                const u = new URL(get('endpoint'));
                ok = (u.protocol === 'https:' || u.protocol === 'http:') && !!u.host;
            } catch {
                ok = false;
            }
            if (!ok) {
                return t('cloudDialog.validation.badEndpoint', {
                    defaultValue: 'Endpoint must be a full URL like https://my-resource.openai.azure.com',
                });
            }
            break;
        }
    }
    return null;
}

/**
 * The cloud SDK adapter derives the real endpoint from the credential fields,
 * but the create API still requires a non-empty api_base. Build a meaningful one
 * from the (trimmed) values so the provider list shows the actual host. The GCP
 * shape mirrors genai's Vertex host rules: "global" and the multi-regional
 * "us"/"eu" locations have dedicated hosts. (Backend template matching keys off
 * auth_type + api_style, not this host.)
 */
export function buildCloudApiBase(authType: string, v: Record<string, string>): string {
    const get = (k: string) => (v[k] || '').trim();
    switch (authType) {
        case 'aws_sigv4':
            return `https://bedrock-runtime.${get('region')}.amazonaws.com`;
        case 'gcp_sa': {
            const loc = get('location');
            if (loc === 'global') return 'https://aiplatform.googleapis.com';
            if (loc === 'us' || loc === 'eu') return `https://aiplatform.${loc}.rep.googleapis.com`;
            return `https://${loc}-aiplatform.googleapis.com`;
        }
        case 'azure_key':
            return get('endpoint').replace(/\/+$/, '');
        default:
            return '';
    }
}
