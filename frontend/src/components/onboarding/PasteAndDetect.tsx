// The "Paste & detect" experience now lives in a shared, shell-agnostic core at
// components/paste-detect/PasteDetectPanel so it can be reused both inline here
// (Onboarding's "Paste & detect" tab) and inside PasteDetectDialog (Connect AI).
// This thin re-export keeps the existing import path stable.
export {default} from '@/components/paste-detect/PasteDetectPanel';
