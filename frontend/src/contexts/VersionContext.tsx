import React, { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react';
import { api } from '../services/api';

interface VersionContextType {
    currentVersion: string;
    latestVersion: string | null;
    hasUpdate: boolean;
    shouldNotify: boolean;
    releaseURL: string | null;
    checking: boolean;
    error: string | null;
    checkForUpdates: (manual?: boolean) => Promise<void>;
    showUpdateDialog: () => void;
    openUpdateDialog: boolean;
    closeUpdateDialog: () => void;
    /** Whether this install shape (npx) supports one-click update. */
    canOneClick: boolean;
    /** One-click update in flight (spawn + waiting for the new version). */
    updating: boolean;
    /** 'timeout' sentinel or a backend error message; null when fine. */
    updateError: string | null;
    applyUpdate: () => Promise<void>;
}

const VersionContext = createContext<VersionContextType | undefined>(undefined);

export const useVersion = () => {
    const context = useContext(VersionContext);
    if (context === undefined) {
        throw new Error('useVersion must be used within a VersionProvider');
    }
    return context;
};

interface VersionProviderProps {
    children: ReactNode;
}

export const VersionProvider: React.FC<VersionProviderProps> = ({ children }) => {
    const [currentVersion, setCurrentVersion] = useState<string>('Unknown');
    const [latestVersion, setLatestVersion] = useState<string | null>(null);
    const [hasUpdate, setHasUpdate] = useState(false);
    const [shouldNotify, setShouldNotify] = useState(false);
    const [releaseURL, setReleaseURL] = useState<string | null>(null);
    const [checking, setChecking] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [openUpdateDialog, setOpenUpdateDialog] = useState(false);
    const [canOneClick, setCanOneClick] = useState(false);
    const [updating, setUpdating] = useState(false);
    const [updateError, setUpdateError] = useState<string | null>(null);

    const checkForUpdates = useCallback(async (manual = false) => {
        setChecking(true);
        setError(null);
        try {
            const result = await api.getLatestVersion();
            if (result && result.success && result.data) {
                // Don't override currentVersion - it should only come from getVersion()
                // The backend current_version may be "dev" in development mode
                setLatestVersion(result.data.latest_version);
                setHasUpdate(result.data.has_update);
                setShouldNotify(result.data.should_notify);
                setReleaseURL(result.data.release_url);
                setCanOneClick(!!result.data.can_one_click);
            }
        } catch (err) {
            console.error('Failed to check for updates:', err);
            const errorMessage = err instanceof Error ? err.message : 'Unknown error';
            setError(errorMessage);
            // Only show error to user if it was a manual check
            if (manual) {
                // Error state is now available via context
            }
        } finally {
            setChecking(false);
        }
    }, []);

    useEffect(() => {
        // Always fetch the locally-known version first — independent of GitHub
        // reachability. The /info/version/check endpoint returns 503 when the
        // GitHub releases lookup fails (offline, rate-limited, dev networks),
        // which would otherwise leave currentVersion stuck on 'Unknown'.
        api.getVersion()
            .then((version) => {
                if (version && version !== 'Unknown') {
                    setCurrentVersion(version);
                }
            })
            .catch(() => {
                /* leave as 'Unknown' */
            });

        // Then check GitHub for updates. May fail; that's fine — currentVersion
        // is already populated from the local endpoint above.
        checkForUpdates(false);

        // Check every 24 hours
        const interval = setInterval(() => checkForUpdates(false), 24 * 60 * 60 * 1000);
        return () => clearInterval(interval);
    }, [checkForUpdates]);

    const applyUpdate = useCallback(async () => {
        if (updating) return;
        setUpdating(true);
        setUpdateError(null);

        const result = await api.applyUpdate();
        if (!result || !result.success) {
            setUpdating(false);
            setUpdateError(result?.error || 'unknown error');
            return;
        }

        // The backend spawned `npx -y tingly-box@<target> restart --daemon`,
        // which downloads the new version and replaces this server. Poll the
        // version endpoint until the new version answers, then reload the
        // page so the UI matches the server it talks to.
        const target: string = result.data?.target_version || '';
        const deadline = Date.now() + 5 * 60 * 1000;
        const poll = async () => {
            try {
                const v = await api.getVersion();
                // Reload only when the served version demonstrably changed:
                // match the apply response's target when we have it; without
                // it, require a real known->different transition — comparing
                // against an unresolved 'Unknown' currentVersion would match
                // the still-running old server on the first poll.
                const known = !!v && v !== 'Unknown';
                const reached = known && (target
                    ? v === target
                    : currentVersion !== 'Unknown' && v !== currentVersion);
                if (reached) {
                    window.location.reload();
                    return;
                }
            } catch {
                // Server restarting — keep polling.
            }
            if (Date.now() < deadline) {
                setTimeout(poll, 3000);
            } else {
                setUpdating(false);
                setUpdateError('timeout');
            }
        };
        // npm needs a moment before anything changes; don't hammer instantly.
        setTimeout(poll, 5000);
    }, [updating, currentVersion]);

    const showUpdateDialog = useCallback(() => {
        setOpenUpdateDialog(true);
    }, []);

    const closeUpdateDialog = useCallback(() => {
        setOpenUpdateDialog(false);
    }, []);

    return (
        <VersionContext.Provider
            value={{
                currentVersion,
                latestVersion,
                hasUpdate,
                shouldNotify,
                releaseURL,
                checking,
                error,
                checkForUpdates,
                showUpdateDialog,
                openUpdateDialog,
                closeUpdateDialog,
                canOneClick,
                updating,
                updateError,
                applyUpdate,
            }}
        >
            {children}
        </VersionContext.Provider>
    );
};
