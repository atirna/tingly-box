import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

// Persisted "is the secondary Sidebar collapsed?" preference. Mirrors the
// ThemeContext pattern (module-level storage key, lazy read, persist-on-change
// effect, Context + guarded hook). Scoped to the layout area: the provider is
// mounted inside <Layout/>, so this hook is only meaningful there.

const STORAGE_KEY = 'layout.sidebarCollapsed';

interface SidebarCollapsedValue {
    collapsed: boolean;
    toggle: () => void;
    setCollapsed: (next: boolean) => void;
}

const SidebarCollapsedContext = createContext<SidebarCollapsedValue | undefined>(undefined);

interface SidebarCollapsedProviderProps {
    children: ReactNode;
}

export const SidebarCollapsedProvider = ({ children }: SidebarCollapsedProviderProps) => {
    const [collapsed, setCollapsed] = useState<boolean>(() => localStorage.getItem(STORAGE_KEY) === 'true');

    useEffect(() => {
        localStorage.setItem(STORAGE_KEY, String(collapsed));
    }, [collapsed]);

    const value = useMemo<SidebarCollapsedValue>(
        () => ({
            collapsed,
            toggle: () => setCollapsed((prev) => !prev),
            setCollapsed,
        }),
        [collapsed],
    );

    return <SidebarCollapsedContext.Provider value={value}>{children}</SidebarCollapsedContext.Provider>;
};

export const useSidebarCollapsed = (): SidebarCollapsedValue => {
    const context = useContext(SidebarCollapsedContext);
    if (!context) {
        throw new Error('useSidebarCollapsed must be used within a SidebarCollapsedProvider');
    }
    return context;
};
