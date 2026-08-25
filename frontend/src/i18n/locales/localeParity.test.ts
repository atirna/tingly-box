import { describe, expect, it } from 'vitest';
import en from './en';
import ru from './ru';
import zh from './zh';

type Bundle = Record<string, unknown>;

// i18next resolves a plural key by appending an Intl.PluralRules category
// suffix. English needs two (_one/_other), Russian four (_one/_few/_many/
// _other) and Chinese none — so a key like `activeUsers_one` in en.ts has no
// single counterpart in the other bundles. Compare on the stem instead.
const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/;

const flatten = (bundle: Bundle, prefix = '', out = new Set<string>()): Set<string> => {
    for (const [key, value] of Object.entries(bundle)) {
        const path = prefix ? `${prefix}.${key}` : key;
        if (value && typeof value === 'object' && !Array.isArray(value)) {
            flatten(value as Bundle, path, out);
        } else {
            out.add(path.replace(PLURAL_SUFFIX, ''));
        }
    }
    return out;
};

const missingFrom = (source: Set<string>, target: Set<string>): string[] =>
    [...source].filter((key) => !target.has(key)).sort();

describe('locale bundles', () => {
    const enKeys = flatten(en as Bundle);
    const zhKeys = flatten(zh as Bundle);
    const ruKeys = flatten(ru as Bundle);

    // en.ts is the fallback bundle, so every key it defines must exist in the
    // other locales or those users silently drop back to English.
    it('ru covers every key in en', () => {
        expect(missingFrom(enKeys, ruKeys)).toEqual([]);
    });

    // Several namespaces (remoteControl.*, bots.*, notify.*) carry their English
    // copy inline as t(..., { defaultValue }) and are only overridden in the
    // non-English bundles; keep ru from falling behind zh on those. There is no
    // en -> zh assertion in return: zh deliberately omits
    // scenarioOverview.descriptions so those fall back to English (see zh.ts).
    it('ru covers every key in zh', () => {
        expect(missingFrom(zhKeys, ruKeys)).toEqual([]);
    });

    it('exposes a native label for every shipped language', () => {
        for (const bundle of [en, ru, zh] as unknown as Bundle[]) {
            const language = (bundle.system as Bundle).language as Record<string, string>;
            expect(language.en).toBe('English');
            expect(language.zh).toBe('中文');
            expect(language.ru).toBe('Русский');
        }
    });
});
