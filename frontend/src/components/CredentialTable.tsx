import {ApiStyleBadge} from "@/components/ApiStyleBadge.tsx";
import {AuthTypeBadge} from "@/components/AuthTypeBadge.tsx";
import ModelListDialog from "@/components/ModelListDialog";
import type {ExportFormat} from "@/components/rule-card/utils";
import {
    exportProviderAsBase64ToClipboard,
    exportProviderAsJsonlToClipboard,
} from "@/components/rule-card/utils";
import {ProviderQuotaDetailRow} from "@/components/credential/ProviderQuotaDetailRow";
import {
    Cancel,
    ContentCopy,
    DataUsage,
    Delete,
    Edit,
    ListAlt,
    MoreVert,
    Refresh as RefreshIcon,
    Route,
    Schedule,
    Visibility,
    VpnKey,
} from '@/components/icons';
import {
    Box,
    Button,
    Chip,
    CircularProgress,
    Divider,
    IconButton,
    Menu,
    MenuItem,
    Modal,
    Paper,
    Stack,
    Switch,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    Tooltip,
    Typography,
} from "@mui/material";
import type {ProviderQuota} from "@/types/quota";
import React, {useCallback, useMemo, useState} from "react";
import api from "../services/api";
import type {Provider} from "../types/provider";

// CredentialTable is the single resource table for every credential —
// OAuth sign-ins and static API keys/tokens alike. Auth mechanism is an
// attribute of a credential (see the Type column), not a reason to split
// into separate tables/cards: both answer the same user question ("what
// can call out on my behalf, and is it healthy?"). Rows are grouped by
// mechanism (OAuth, then static keys) purely for scanability — see
// groupAndSort — while everything else (columns, actions, dialogs) is
// shared across both.
const COLUMN_COUNT = 8;

interface CredentialTableProps {
    providers: Provider[];
    onEdit?: (providerUuid: string) => void;
    onToggle?: (providerUuid: string) => void;
    onDelete?: (providerUuid: string) => void;
    onReauthorize?: (providerUuid: string) => void;
    onRefreshToken?: (providerUuid: string) => Promise<void>;
    onNotification?: (message: string, severity: "success" | "error") => void;
    providerQuotas?: { [uuid: string]: ProviderQuota };
    refreshingQuotas?: Set<string>;
    onQuotaRefresh?: (providerUuid: string) => void;
}

interface DeleteModalState {
    open: boolean;
    providerUuid: string;
    providerName: string;
}

interface RefreshModalState {
    open: boolean;
    providerUuid: string;
    providerName: string;
}

interface TokenModalState {
    open: boolean;
    providerName: string;
    token: string;
    loading: boolean;
}

interface ModelListDialogState {
    open: boolean;
    provider: Provider | null;
}

const isOAuth = (provider: Provider) => provider.auth_type === "oauth";

interface CredentialGroup {
    key: string;
    label: string;
    items: Provider[];
}

// OAuth first (interactive sign-ins users come back to reauthorize/refresh),
// then static credentials. Within a group, enabled credentials surface
// before disabled ones, then alphabetically.
const groupAndSort = (providers: Provider[]): CredentialGroup[] => {
    const groups: CredentialGroup[] = [
        {key: "oauth", label: "OAuth", items: []},
        {key: "api_key", label: "API Key", items: []},
    ];
    for (const provider of providers) {
        (isOAuth(provider) ? groups[0] : groups[1]).items.push(provider);
    }
    for (const group of groups) {
        group.items.sort((a, b) => {
            if (a.enabled !== b.enabled) return a.enabled ? -1 : 1;
            return (a.name || "").localeCompare(b.name || "");
        });
    }
    return groups.filter((group) => group.items.length > 0);
};

const CredentialTable = ({
                              providers,
                              onEdit,
                              onToggle,
                              onDelete,
                              onReauthorize,
                              onRefreshToken,
                              onNotification,
                              providerQuotas,
                              refreshingQuotas,
                              onQuotaRefresh,
                          }: CredentialTableProps) => {
    const groups = useMemo(() => groupAndSort(providers), [providers]);

    const [deleteModal, setDeleteModal] = useState<DeleteModalState>({
        open: false,
        providerUuid: "",
        providerName: "",
    });

    const [refreshModal, setRefreshModal] = useState<RefreshModalState>({
        open: false,
        providerUuid: "",
        providerName: "",
    });

    const [refreshing, setRefreshing] = useState<string | null>(null);

    const [tokenModal, setTokenModal] = useState<TokenModalState>({
        open: false,
        providerName: "",
        token: "",
        loading: false,
    });

    const [modelListDialog, setModelListDialog] = useState<ModelListDialogState>({
        open: false,
        provider: null,
    });
    const [moreMenu, setMoreMenu] = useState<{
        anchorEl: HTMLElement | null;
        providerUuid: string;
    }>({
        anchorEl: null,
        providerUuid: "",
    });

    const handleMoreOpen = (
        e: React.MouseEvent<HTMLElement>,
        providerUuid: string,
    ) => {
        e.stopPropagation();
        setMoreMenu({anchorEl: e.currentTarget, providerUuid});
    };
    const handleMoreClose = () =>
        setMoreMenu({anchorEl: null, providerUuid: ""});

    const handleDeleteClick = (providerUuid: string) => {
        const provider = providers.find((p) => p.uuid === providerUuid);
        setDeleteModal({
            open: true,
            providerUuid,
            providerName: provider?.name || "Unknown Provider",
        });
    };

    const handleCloseDeleteModal = () => {
        setDeleteModal({open: false, providerUuid: "", providerName: ""});
    };

    const handleConfirmDelete = () => {
        if (onDelete && deleteModal.providerUuid) {
            onDelete(deleteModal.providerUuid);
        }
        handleCloseDeleteModal();
    };

    const handleRefreshClick = (providerUuid: string) => {
        const provider = providers.find((p) => p.uuid === providerUuid);
        setRefreshModal({
            open: true,
            providerUuid,
            providerName: provider?.name || "Unknown Provider",
        });
    };

    const handleCloseRefreshModal = () => {
        setRefreshModal({open: false, providerUuid: "", providerName: ""});
    };

    const handleConfirmRefresh = async () => {
        if (!onRefreshToken || !refreshModal.providerUuid) return;

        setRefreshing(refreshModal.providerUuid);
        try {
            await onRefreshToken(refreshModal.providerUuid);
        } finally {
            setRefreshing(null);
        }
        handleCloseRefreshModal();
    };

    const fetchFullToken = async (providerUuid: string): Promise<string> => {
        const response = await api.getProvider(providerUuid);
        if (!response.success) {
            throw new Error(`Failed to fetch token for provider ${providerUuid}`);
        }
        return response.data.token || "";
    };

    const handleViewToken = async (providerUuid: string) => {
        setTokenModal({open: true, providerName: "", token: "", loading: true});
        try {
            const [fullToken, providerResponse] = await Promise.all([
                fetchFullToken(providerUuid),
                api.getProvider(providerUuid),
            ]);
            if (providerResponse.success) {
                setTokenModal({
                    open: true,
                    providerName: providerResponse.data.name,
                    token: fullToken,
                    loading: false,
                });
            }
        } catch (error) {
            console.error("Failed to fetch token:", error);
            setTokenModal({open: true, providerName: "", token: "", loading: false});
        }
    };

    const handleCloseTokenModal = () => {
        setTokenModal({open: false, providerName: "", token: "", loading: false});
    };

    const formatTokenDisplay = (provider: Provider) => {
        if (!provider.token) return "Not set";
        if (provider.token.length <= 12) return provider.token;
        const prefix = provider.token.substring(0, 4);
        const suffix = provider.token.substring(provider.token.length - 4);
        return `${prefix}${"*".repeat(4)}${suffix}`;
    };

    const handleModelListClick = (providerUuid: string) => {
        const provider = providers.find((p) => p.uuid === providerUuid);
        if (provider) {
            setModelListDialog({open: true, provider});
        }
    };

    const handleCloseModelListDialog = () => {
        setModelListDialog({open: false, provider: null});
    };

    const handleCopyProviderBase64 = useCallback(
        async (provider: Provider) => {
            await exportProviderAsBase64ToClipboard(provider, (message, severity) => {
                onNotification?.(message, severity);
            });
        },
        [onNotification],
    );

    const handleCopyProviderJsonl = useCallback(
        async (provider: Provider) => {
            await exportProviderAsJsonlToClipboard(provider, (message, severity) => {
                onNotification?.(message, severity);
            });
        },
        [onNotification],
    );

    const formatExpiresAt = (expiresAt?: string) => {
        if (!expiresAt) return "Never";
        const date = new Date(expiresAt);
        const now = new Date();
        const isExpired = date < now;

        const diffMs = date.getTime() - now.getTime();
        const diffMins = Math.floor(diffMs / 60000);
        const diffHours = Math.floor(diffMs / 3600000);
        const diffDays = Math.floor(diffMs / 86400000);

        if (isExpired) {
            return "Expired";
        } else if (diffMins < 60) {
            return `in ${diffMins} min`;
        } else if (diffHours < 24) {
            return `in ${diffHours}h`;
        } else if (diffDays < 7) {
            return `in ${diffDays} days`;
        } else {
            return date.toLocaleDateString();
        }
    };

    const getExpirationColor = (expiresAt?: string) => {
        if (!expiresAt) return "default";
        const date = new Date(expiresAt);
        const now = new Date();
        const diffHours = (date.getTime() - now.getTime()) / 3600000;

        if (date < now) return "error";
        if (diffHours < 1) return "error";
        if (diffHours < 24) return "warning";
        return "success";
    };

    return (
        <TableContainer
            component={Paper}
            elevation={0}
            sx={{
                border: 1,
                borderColor: "divider",
                borderRadius: 2,
                boxShadow: "none",
                overflowX: "auto",
            }}
        >
            <Table sx={{tableLayout: "fixed", minWidth: 1200}}>
                <TableHead>
                    <TableRow sx={{bgcolor: "action.hover"}}>
                        <TableCell sx={{fontWeight: 600, width: 90, py: 1.25}}>Status</TableCell>
                        <TableCell sx={{fontWeight: 600, width: 150, py: 1.25}}>Name</TableCell>
                        <TableCell sx={{fontWeight: 600, width: 90, py: 1.25}}>Type</TableCell>
                        <TableCell sx={{fontWeight: 600, width: 130, py: 1.25}}>API Style</TableCell>
                        <TableCell sx={{fontWeight: 600, width: 190, py: 1.25}}>API Base URL</TableCell>
                        <TableCell sx={{fontWeight: 600, width: 170, py: 1.25}}>Credential</TableCell>
                        <TableCell sx={{fontWeight: 600, width: 60, py: 1.25}}>Proxy</TableCell>
                        <TableCell sx={{fontWeight: 600, width: 250, py: 1.25}}>Actions</TableCell>
                    </TableRow>
                </TableHead>
                <TableBody>
                    {groups.map((group) => (
                        <React.Fragment key={group.key}>
                            {/* Group label — only worth the row when the list is actually
                                mixed; a single-mechanism list needs no extra chrome. */}
                            {groups.length > 1 && (
                                <TableRow sx={{bgcolor: "action.hover"}}>
                                    <TableCell colSpan={COLUMN_COUNT} sx={{py: 0.75, borderBottom: "none"}}>
                                        <Stack direction="row" spacing={1} sx={{alignItems: "center"}}>
                                            <Typography
                                                variant="caption"
                                                sx={{
                                                    fontWeight: 700,
                                                    letterSpacing: 0.5,
                                                    textTransform: "uppercase",
                                                    color: "text.secondary",
                                                }}
                                            >
                                                {group.label}
                                            </Typography>
                                            <Chip
                                                label={group.items.length}
                                                size="small"
                                                variant="outlined"
                                                sx={{height: 16, minWidth: 16, fontSize: "0.65rem"}}
                                            />
                                        </Stack>
                                    </TableCell>
                                </TableRow>
                            )}
                            {group.items.map((provider) => {
                                const oauth = isOAuth(provider);
                                const expiresAt = provider.oauth_detail?.expires_at;

                                return (
                                    <React.Fragment key={provider.uuid}>
                                        <TableRow
                                            hover
                                            sx={{
                                                "& > .MuiTableCell-root": {
                                                    py: 1.25,
                                                },
                                            }}
                                        >
                                            {/* Status */}
                                            <TableCell>
                                                <Stack direction="row" spacing={1} sx={{alignItems: "center"}}>
                                                    <Switch
                                                        checked={provider.enabled}
                                                        onChange={() => onToggle?.(provider.uuid)}
                                                        size="small"
                                                        color="success"
                                                    />
                                                    <Chip
                                                        label={provider.enabled ? "On" : "Off"}
                                                        size="small"
                                                        color={provider.enabled ? "success" : "default"}
                                                        variant={provider.enabled ? "filled" : "outlined"}
                                                        sx={{height: 22, minWidth: 40}}
                                                    />
                                                </Stack>
                                            </TableCell>
                                            {/* Name */}
                                            <TableCell>
                                                <Tooltip title={provider.name} arrow placement="top">
                                                    <Typography
                                                        variant="body2"
                                                        sx={{
                                                            fontWeight: 500,
                                                            maxWidth: 130,
                                                            overflow: "hidden",
                                                            textOverflow: "ellipsis",
                                                            whiteSpace: "nowrap",
                                                        }}
                                                    >
                                                        {provider.name}
                                                    </Typography>
                                                </Tooltip>
                                            </TableCell>
                                            {/* Type */}
                                            <TableCell>
                                                <AuthTypeBadge authType={provider.auth_type || "api_key"}/>
                                            </TableCell>
                                            {/* API Style */}
                                            <TableCell>
                                                {provider.api_base_openai && provider.api_base_anthropic ? (
                                                    <Stack direction="column" spacing={0.5} sx={{alignItems: "flex-start"}}>
                                                        <ApiStyleBadge
                                                            apiStyle="openai"
                                                            sx={{minWidth: "100px", justifyContent: "center"}}
                                                        />
                                                        <ApiStyleBadge
                                                            apiStyle="anthropic"
                                                            sx={{minWidth: "100px", justifyContent: "center"}}
                                                        />
                                                    </Stack>
                                                ) : (
                                                    <ApiStyleBadge
                                                        sx={{minWidth: "100px"}}
                                                        apiStyle={provider.api_style}
                                                    />
                                                )}
                                            </TableCell>
                                            {/* API Base URL */}
                                            <TableCell>
                                                <Typography
                                                    variant="body2"
                                                    sx={{
                                                        maxWidth: 170,
                                                        fontFamily: "monospace",
                                                        wordBreak: "break-all",
                                                    }}
                                                >
                                                    {provider.api_base}
                                                </Typography>
                                            </TableCell>
                                            {/* Credential — expiry for OAuth, masked secret for static keys */}
                                            <TableCell>
                                                {oauth ? (
                                                    <Stack direction="row" spacing={1} sx={{alignItems: "center"}}>
                                                        <Schedule
                                                            fontSize="small"
                                                            color={getExpirationColor(expiresAt) as any}
                                                        />
                                                        <Typography
                                                            variant="body2"
                                                            color={(getExpirationColor(expiresAt) + ".main") as any}
                                                        >
                                                            {formatExpiresAt(expiresAt)}
                                                        </Typography>
                                                    </Stack>
                                                ) : (
                                                    <Stack direction="row" spacing={1} sx={{alignItems: "center"}}>
                                                        {provider.token && (
                                                            <Tooltip title="View Token">
                                                                <IconButton
                                                                    aria-label={`View API key for ${provider.name}`}
                                                                    size="small"
                                                                    onClick={() => handleViewToken(provider.uuid)}
                                                                    sx={{p: 0.25}}
                                                                >
                                                                    <Visibility fontSize="small"/>
                                                                </IconButton>
                                                            </Tooltip>
                                                        )}
                                                        <Typography
                                                            variant="body2"
                                                            sx={{
                                                                fontFamily: "monospace",
                                                                wordBreak: "break-all",
                                                                flex: 1,
                                                                minWidth: 0,
                                                            }}
                                                        >
                                                            {formatTokenDisplay(provider)}
                                                        </Typography>
                                                    </Stack>
                                                )}
                                            </TableCell>
                                            {/* Proxy */}
                                            <TableCell align="center">
                                                {provider.proxy_url ? (
                                                    <Tooltip title={provider.proxy_url} arrow>
                                                        <Route fontSize="small" sx={{color: "text.secondary"}}/>
                                                    </Tooltip>
                                                ) : (
                                                    <Typography variant="body2" sx={{color: "text.secondary"}}>
                                                        -
                                                    </Typography>
                                                )}
                                            </TableCell>
                                            {/* Actions */}
                                            <TableCell sx={{whiteSpace: "nowrap"}}>
                                                <Box
                                                    sx={{
                                                        display: "flex",
                                                        alignItems: "center",
                                                        gap: 0.5,
                                                        border: 1,
                                                        borderColor: "divider",
                                                        borderRadius: 1.5,
                                                        p: 0.5,
                                                        width: "fit-content",
                                                    }}
                                                >
                                                    {/* Edit — primary action, always visible */}
                                                    {onEdit && (
                                                        <Tooltip title={oauth ? "View Details" : "Edit"}>
                                                            <IconButton
                                                                aria-label={`${oauth ? "View details for" : "Edit"} ${provider.name}`}
                                                                size="small"
                                                                color="primary"
                                                                onClick={() => onEdit(provider.uuid)}
                                                            >
                                                                <Edit fontSize="small"/>
                                                            </IconButton>
                                                        </Tooltip>
                                                    )}
                                                    <Divider orientation="vertical" flexItem/>
                                                    {/* Quota text button */}
                                                    {onQuotaRefresh && (
                                                        <Button
                                                            variant="text"
                                                            size="small"
                                                            startIcon={
                                                                refreshingQuotas?.has(provider.uuid) ? (
                                                                    <CircularProgress size={12}/>
                                                                ) : (
                                                                    <DataUsage fontSize="small"/>
                                                                )
                                                            }
                                                            onClick={() => onQuotaRefresh(provider.uuid)}
                                                            disabled={refreshingQuotas?.has(provider.uuid)}
                                                            color={providerQuotas?.[provider.uuid] ? "primary" : "inherit"}
                                                            sx={{minWidth: "auto", px: 1}}
                                                        >
                                                            Quota
                                                        </Button>
                                                    )}
                                                    {/* Models text button */}
                                                    <Button
                                                        variant="text"
                                                        size="small"
                                                        startIcon={<ListAlt/>}
                                                        onClick={() => handleModelListClick(provider.uuid)}
                                                        disabled={!provider.enabled}
                                                        sx={{fontSize: "0.75rem", minWidth: "auto", px: 1}}
                                                    >
                                                        Models
                                                    </Button>
                                                    <Divider orientation="vertical" flexItem/>
                                                    {/* Overflow menu */}
                                                    <IconButton
                                                        aria-label={`More actions for ${provider.name}`}
                                                        size="small"
                                                        onClick={(e) => handleMoreOpen(e, provider.uuid)}
                                                    >
                                                        <MoreVert fontSize="small"/>
                                                    </IconButton>
                                                </Box>
                                            </TableCell>
                                        </TableRow>
                                        {/* Quota detail row */}
                                        {providerQuotas && onQuotaRefresh && (
                                            <ProviderQuotaDetailRow
                                                provider={provider}
                                                quota={providerQuotas[provider.uuid]}
                                                isRefreshing={refreshingQuotas?.has(provider.uuid) || false}
                                                onRefresh={onQuotaRefresh}
                                                colSpan={COLUMN_COUNT}
                                            />
                                        )}
                                    </React.Fragment>
                                );
                            })}
                        </React.Fragment>
                    ))}
                </TableBody>
            </Table>
            {/* Overflow menu (shared) */}
            <Menu
                anchorEl={moreMenu.anchorEl}
                open={Boolean(moreMenu.anchorEl)}
                onClose={handleMoreClose}
                onClick={(e) => e.stopPropagation()}
                anchorOrigin={{vertical: "bottom", horizontal: "right"}}
                transformOrigin={{vertical: "top", horizontal: "right"}}
            >
                {(() => {
                    const p = providers.find((p) => p.uuid === moreMenu.providerUuid);
                    if (!p) return null;
                    const oauth = isOAuth(p);
                    const hasRefreshToken = oauth && onRefreshToken && p.oauth_detail?.refresh_token;
                    const expired = oauth && p.oauth_detail?.expires_at
                        ? new Date(p.oauth_detail.expires_at) < new Date()
                        : false;
                    return [
                        hasRefreshToken && (
                            <MenuItem
                                key="refresh-token"
                                onClick={() => {
                                    handleMoreClose();
                                    handleRefreshClick(p.uuid);
                                }}
                                disabled={refreshing === p.uuid}
                            >
                                {refreshing === p.uuid ? (
                                    <CircularProgress size={14} sx={{mr: 1}}/>
                                ) : (
                                    <RefreshIcon fontSize="small" sx={{mr: 1}}/>
                                )}
                                Refresh Token
                            </MenuItem>
                        ),
                        oauth && onReauthorize && (
                            <MenuItem
                                key="reauthorize"
                                onClick={() => {
                                    handleMoreClose();
                                    onReauthorize(p.uuid);
                                }}
                                sx={{color: expired ? "warning.main" : undefined}}
                            >
                                <VpnKey fontSize="small" sx={{mr: 1}}/> Reauthorize
                            </MenuItem>
                        ),
                        oauth && (hasRefreshToken || onReauthorize) && <Divider key="div-oauth"/>,
                        !oauth && p.token && (
                            <MenuItem
                                key="view-token"
                                onClick={() => {
                                    handleMoreClose();
                                    handleViewToken(p.uuid);
                                }}
                            >
                                <Visibility fontSize="small" sx={{mr: 1}}/> View Token
                            </MenuItem>
                        ),
                        <MenuItem
                            key="copy-base64"
                            onClick={() => {
                                handleMoreClose();
                                handleCopyProviderBase64(p);
                            }}
                        >
                            <ContentCopy fontSize="small" sx={{mr: 1}}/> Copy Base64
                        </MenuItem>,
                        <MenuItem
                            key="copy-jsonl"
                            onClick={() => {
                                handleMoreClose();
                                handleCopyProviderJsonl(p);
                            }}
                        >
                            <ContentCopy fontSize="small" sx={{mr: 1}}/> Copy JSONL
                        </MenuItem>,
                        onDelete && <Divider key="div2"/>,
                        onDelete && (
                            <MenuItem
                                key="delete"
                                onClick={() => {
                                    handleMoreClose();
                                    handleDeleteClick(p.uuid);
                                }}
                                sx={{color: "error.main"}}
                            >
                                <Delete fontSize="small" sx={{mr: 1}}/> Delete
                            </MenuItem>
                        ),
                    ].filter(Boolean);
                })()}
            </Menu>
            {/* Delete Confirmation Modal */}
            <Modal open={deleteModal.open} onClose={handleCloseDeleteModal}>
                <Box
                    sx={{
                        position: "absolute",
                        top: "50%",
                        left: "50%",
                        transform: "translate(-50%, -50%)",
                        width: 400,
                        maxWidth: "80vw",
                        bgcolor: "background.paper",
                        boxShadow: 24,
                        p: 4,
                        borderRadius: 2,
                    }}
                >
                    <Typography variant="h6" sx={{mb: 2}}>
                        Delete Credential
                    </Typography>
                    <Typography variant="body2" sx={{mb: 3}}>
                        Are you sure you want to delete the credential "
                        {deleteModal.providerName}"? This action cannot be undone.
                    </Typography>
                    <Stack direction="row" spacing={2} sx={{justifyContent: "flex-end"}}>
                        <Button onClick={handleCloseDeleteModal} color="inherit">
                            Cancel
                        </Button>
                        <Button onClick={handleConfirmDelete} color="error" variant="contained">
                            Delete
                        </Button>
                    </Stack>
                </Box>
            </Modal>
            {/* Refresh Token Confirmation Modal */}
            <Modal open={refreshModal.open} onClose={handleCloseRefreshModal}>
                <Box
                    sx={{
                        position: "absolute",
                        top: "50%",
                        left: "50%",
                        transform: "translate(-50%, -50%)",
                        width: 400,
                        maxWidth: "80vw",
                        bgcolor: "background.paper",
                        boxShadow: 24,
                        p: 4,
                        borderRadius: 2,
                    }}
                >
                    <Typography variant="h6" sx={{mb: 2}}>
                        Refresh OAuth Token
                    </Typography>
                    <Typography variant="body2" sx={{mb: 3}}>
                        Are you sure you want to refresh the OAuth token for "
                        {refreshModal.providerName}"? This will update the access token
                        using the refresh token.
                    </Typography>
                    <Stack direction="row" spacing={2} sx={{justifyContent: "flex-end"}}>
                        <Button
                            onClick={handleCloseRefreshModal}
                            color="inherit"
                            disabled={refreshing !== null}
                        >
                            Cancel
                        </Button>
                        <Button
                            onClick={handleConfirmRefresh}
                            color="info"
                            variant="contained"
                            disabled={refreshing !== null}
                            startIcon={
                                refreshing !== null ? (
                                    <CircularProgress size={16}/>
                                ) : (
                                    <RefreshIcon fontSize="small"/>
                                )
                            }
                        >
                            {refreshing !== null ? "Refreshing..." : "Refresh"}
                        </Button>
                    </Stack>
                </Box>
            </Modal>
            {/* Token View Modal */}
            <Modal open={tokenModal.open} onClose={handleCloseTokenModal}>
                <Box
                    sx={{
                        position: "absolute",
                        top: "50%",
                        left: "50%",
                        transform: "translate(-50%, -50%)",
                        width: 600,
                        maxWidth: "80vw",
                        bgcolor: "background.paper",
                        boxShadow: 24,
                        p: 4,
                        borderRadius: 2,
                    }}
                >
                    <Typography variant="h6" sx={{mb: 2}}>
                        {tokenModal.token
                            ? `API Key - ${tokenModal.providerName}`
                            : tokenModal.providerName}
                    </Typography>

                    {tokenModal.loading ? (
                        <Box sx={{mb: 3, textAlign: "center", py: 4}}>
                            <Typography variant="body2" sx={{color: "text.secondary"}}>
                                Loading API key...
                            </Typography>
                        </Box>
                    ) : tokenModal.token ? (
                        <Box sx={{mb: 3}}>
                            <Box
                                sx={{
                                    p: 2,
                                    bgcolor: "action.hover",
                                    borderRadius: 1,
                                    fontFamily: "monospace",
                                    wordBreak: "break-all",
                                    border: "1px solid",
                                    borderColor: "divider",
                                }}
                            >
                                {tokenModal.token}
                            </Box>
                        </Box>
                    ) : null}

                    <Stack direction="row" spacing={2} sx={{justifyContent: "flex-end"}}>
                        <IconButton
                            aria-label={`Copy API key for ${tokenModal.providerName || "provider"}`}
                            color="primary"
                            disabled={tokenModal.loading || !tokenModal.token}
                            onClick={async () => {
                                if (tokenModal.token) {
                                    try {
                                        await navigator.clipboard.writeText(tokenModal.token);
                                    } catch (err) {
                                        console.error("Failed to copy token:", err);
                                    }
                                }
                            }}
                            title={tokenModal.loading ? "Loading..." : "Copy Token"}
                        >
                            <ContentCopy/>
                        </IconButton>
                        <Tooltip title="Close">
                            <IconButton aria-label="Close API key dialog" onClick={handleCloseTokenModal}>
                                <Cancel/>
                            </IconButton>
                        </Tooltip>
                    </Stack>
                </Box>
            </Modal>
            {/* Model List Dialog */}
            <ModelListDialog
                open={modelListDialog.open}
                onClose={handleCloseModelListDialog}
                provider={modelListDialog.provider}
            />
        </TableContainer>
    );
};

export default CredentialTable;
