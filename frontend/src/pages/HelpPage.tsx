import CardGrid from '@/components/CardGrid.tsx';
import { PageLayout } from '@/components/PageLayout.tsx';
import { ShortcutCard } from '@/components/ShortcutCard.tsx';
import { useTranslation } from 'react-i18next';

// Cap each card's width on wide viewports, matching System settings cards.
const CARD_MAX_WIDTH = 720;

/**
 * HelpPage — a small collection of easy-to-miss, useful actions, reached via
 * the lightbulb entry in the activity bar. Each card here is deliberately a
 * standalone, re-entrant action (not a step in a linear tour) — the page has
 * no "done" state and nothing to complete in order.
 */
const HelpPage = () => {
    const { t } = useTranslation();

    return (
        <PageLayout loading={false} title={t('help.title')} subtitle={t('help.description')}>
            <CardGrid>
                <ShortcutCard maxWidth={CARD_MAX_WIDTH} />
            </CardGrid>
        </PageLayout>
    );
};

export default HelpPage;
