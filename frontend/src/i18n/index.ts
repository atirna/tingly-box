import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import en from './locales/en';
import zh from './locales/zh';

const resources = {
    en: {
        translation: en,
    },
    zh: {
        translation: zh,
    },
};

// Detect a supported language from the browser's configured languages,
// falling back to English when nothing matches.
const detectBrowserLanguage = (): 'en' | 'zh' => {
    const browserLangs = navigator.languages && navigator.languages.length > 0 ? navigator.languages : [navigator.language];
    for (const lang of browserLangs) {
        if (!lang) continue;
        const normalized = lang.toLowerCase();
        if (normalized.startsWith('zh')) return 'zh';
        if (normalized.startsWith('en')) return 'en';
    }
    return 'en';
};

// Custom language detector: an explicit user choice (persisted to localStorage
// by i18n.changeLanguage) always wins; otherwise fall back to the browser's language.
const languageDetectorOptions = {
    // Order and sources where to look for language
    order: ['localStorage'],
    // Keys or params to lookup language from
    lookupLocalStorage: 'i18nextLng',
    // Cache user language
    caches: ['localStorage'],
    // Custom detection function - check localStorage first, else detect from the browser
    detection: () => {
        const stored = localStorage.getItem('i18nextLng');
        if (stored && (stored === 'en' || stored === 'zh')) {
            return stored;
        }
        return detectBrowserLanguage();
    },
};

i18n
    .use(LanguageDetector) // Detect user language
    .use(initReactI18next) // Passes i18n down to react-i18next
    .init({
        resources,
        fallbackLng: 'en', // Use English by default
        defaultNS: 'translation',
        debug: false,

        // Configure language detection and storage
        detection: languageDetectorOptions,

        interpolation: {
            escapeValue: false, // React already escapes values
        },
    });

export default i18n;
