import {Message as MessageIcon, ContentCopy as CopyIcon, Block as BlockIcon, Delete as DeleteIcon} from '@/components/icons';
import type {BotChat} from '@/types/bot';
import {api} from '@/services/api';
import {notify} from '@/utils/notify';
import {
    Box,
    Button,
    CircularProgress,
    Dialog,
    DialogActions,
    DialogContent,
    DialogContentText,
    DialogTitle,
    IconButton,
    Link,
    List,
    ListItem,
    Stack,
    Tooltip,
    Typography,
} from '@mui/material';
import Popover from '@mui/material/Popover';
import {useCallback, useState} from 'react';
import {useTranslation} from 'react-i18next';

// BotChatsButton surfaces the chat ids a bot can reach, so an operator can
// copy the channel-native chat_id that POST /api/v1/bots/:bot/{notify,interact}
// requires in its request body. Without this the value is undiscoverable from
// the UI (see ux-principles #5 — show the concrete value; #11 — hand over the
// artifact for the next action). Loads lazily on first open via the
// /bots/:bot/chats endpoint.
//
// Each row also carries the chat's lifecycle actions: disable (blocklist —
// inbound messages are silently dropped and the chat leaves this list) and
// delete (hard-remove the record; the chat re-registers fresh if it messages
// again). Disabled chats are hidden by default and revealed by the footer
// toggle, rendered dimmed with an enable action.
interface BotChatsButtonProps {
    botUUID: string;
    // platform + pairingRequired tailor the empty-chats hint: a bot that
    // enforces TOFU pairing registers a chat only after the user pairs first,
    // so the hint points there instead of just "send a message".
    platform?: string;
    pairingRequired?: boolean;
    disabled?: boolean;
}

const BotChatsButton: React.FC<BotChatsButtonProps> = ({botUUID, platform, pairingRequired, disabled}) => {
    const {t} = useTranslation();
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    const [tooltipOpen, setTooltipOpen] = useState(false);
    const [loading, setLoading] = useState(false);
    const [chats, setChats] = useState<BotChat[]>([]);
    const [running, setRunning] = useState<boolean | null>(null); // null = not loaded yet
    const [error, setError] = useState<string | null>(null);
    const [showDisabled, setShowDisabled] = useState(false);
    const [busyChat, setBusyChat] = useState<string | null>(null); // chat_id with an in-flight mutation
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null); // chat_id pending confirm

    const load = useCallback(async (includeDisabled: boolean) => {
        setLoading(true);
        setError(null);
        // Always fetch with include_disabled — the visible set is filtered
        // client-side, so the footer toggle and post-mutation refreshes don't
        // need extra round-trips to know how many disabled chats exist.
        const result = await api.listBotChats(botUUID, true);
        setLoading(false);
        if (result.error) {
            setError(result.error);
        } else {
            setChats(result.chats ?? []);
            setRunning(result.running ?? true);
            setShowDisabled(includeDisabled);
        }
    }, [botUUID]);

    const handleOpen = useCallback(async (e: React.MouseEvent<HTMLElement>) => {
        // Close the tooltip the moment the popover opens, otherwise its hover
        // text lingers over the open popover.
        setTooltipOpen(false);
        setAnchor(e.currentTarget);
        if (running !== null || error) return; // already loaded this session
        await load(false);
    }, [running, error, load]);

    const handleClose = useCallback(() => setAnchor(null), []);

    const handleCopy = useCallback(async (chatID: string) => {
        try {
            await navigator.clipboard.writeText(chatID);
            notify.success(t('bots.table.chatIdCopied', {defaultValue: 'Chat ID copied'}));
        } catch {
            notify.error(t('bots.table.chatIdCopyFailed', {defaultValue: 'Copy failed — check clipboard permissions'}));
        }
    }, [t]);

    const handleToggleDisabled = useCallback(async (chat: BotChat) => {
        const target = !chat.disabled;
        setBusyChat(chat.chat_id);
        const result = await api.setBotChatDisabled(botUUID, chat.chat_id, target);
        setBusyChat(null);
        if (result.error) {
            notify.error(result.error);
            return;
        }
        notify.success(target
            ? t('bots.table.chatDisabled', {defaultValue: 'Chat disabled — its messages are now dropped'})
            : t('bots.table.chatEnabled', {defaultValue: 'Chat re-enabled'}));
        setChats(prev => prev.map(c => c.chat_id === chat.chat_id ? {...c, disabled: target} : c));
    }, [botUUID, t]);

    const handleDelete = useCallback(async (chatID: string) => {
        setDeleteTarget(null);
        setBusyChat(chatID);
        const result = await api.deleteBotChat(botUUID, chatID);
        setBusyChat(null);
        if (result.error) {
            notify.error(result.error);
            return;
        }
        notify.success(t('bots.table.chatDeleted', {defaultValue: 'Chat deleted'}));
        setChats(prev => prev.filter(c => c.chat_id !== chatID));
    }, [botUUID, t]);

    const open = Boolean(anchor);
    const disabledCount = chats.filter(c => c.disabled).length;
    const visibleChats = showDisabled ? chats : chats.filter(c => !c.disabled);

    return (
        <>
            <Popover
                open={open}
                anchorEl={anchor}
                onClose={handleClose}
                anchorOrigin={{vertical: 'bottom', horizontal: 'left'}}
                transformOrigin={{vertical: 'top', horizontal: 'left'}}
                slotProps={{paper: {sx: {minWidth: 320, maxWidth: 420, mt: 0.5}}}}
            >
                <Box sx={{p: 1.5}}>
                    <Typography variant="caption" sx={{color: 'text.secondary', fontWeight: 600}}>
                        {t('bots.table.chatsTitle', {defaultValue: 'Reachable chats — copy the Chat ID for notify/interact'})}
                    </Typography>
                    {loading && (
                        <Box sx={{display: 'flex', justifyContent: 'center', py: 2}}>
                            <CircularProgress size={20}/>
                        </Box>
                    )}
                    {!loading && error && (
                        <Typography variant="body2" sx={{color: 'error.main', py: 1}}>{error}</Typography>
                    )}
                    {!loading && !error && visibleChats.length === 0 && (
                        // Empty state names the mechanism that registers a chat
                        // (not just "no chats") and points to the next action.
                        // A bot that isn't running has no reachable chats at
                        // all, so the message points to starting it rather
                        // than sending a message into the void.
                        // ux-principles #11: hand over the next action, not a notice.
                        <Box sx={{py: 1}}>
                            <Typography variant="body2" sx={{color: 'text.disabled'}}>
                                {running === false
                                    ? t('bots.table.notRunning', {
                                        defaultValue: 'This bot isn’t running. Start it, then send it a message on {{platform}} — its Chat ID appears here.',
                                        platform: platform || t('bots.table.itsPlatform', {defaultValue: 'its platform'}),
                                    })
                                    : pairingRequired
                                        ? t('bots.table.noChatsPairFirst', {
                                            defaultValue: 'No chats yet. Pair this bot (see Pairing), then send it a message on {{platform}} — its Chat ID appears here.',
                                            platform: platform || t('bots.table.itsPlatform', {defaultValue: 'its platform'}),
                                        })
                                        : t('bots.table.noChats', {
                                            defaultValue: 'No chats yet. Send any message to this bot on {{platform}} and its Chat ID will appear here.',
                                            platform: platform || t('bots.table.itsPlatform', {defaultValue: 'its platform'}),
                                        })}
                            </Typography>
                        </Box>
                    )}
                    {!loading && !error && visibleChats.length > 0 && (
                        <List dense disablePadding sx={{mt: 0.5}}>
                            {visibleChats.map((chat) => (
                                <ListItem key={chat.chat_id} disableGutters sx={{py: 0.25}}>
                                    <Stack direction="row" spacing={0.5} sx={{alignItems: 'center', width: '100%', minWidth: 0}}>
                                        <IconButton
                                            size="small"
                                            onClick={() => handleCopy(chat.chat_id)}
                                            sx={{p: 0.25, flexShrink: 0}}
                                            aria-label={t('bots.table.copyChatId', {defaultValue: 'Copy Chat ID'})}
                                        >
                                            <CopyIcon fontSize="inherit"/>
                                        </IconButton>
                                        <Typography
                                            variant="caption"
                                            component="span"
                                            sx={{
                                                fontFamily: 'monospace',
                                                overflow: 'hidden',
                                                textOverflow: 'ellipsis',
                                                whiteSpace: 'nowrap',
                                                minWidth: 0,
                                                flex: 1,
                                                // A disabled chat stays legible (its id may still need
                                                // copying to re-enable via API) but is visibly parked.
                                                ...(chat.disabled && {color: 'text.disabled', textDecoration: 'line-through'}),
                                            }}
                                        >
                                            {chat.chat_id}
                                        </Typography>
                                        {chat.is_paired && !chat.disabled && (
                                            <Typography variant="caption" sx={{color: 'success.main', flexShrink: 0}}>
                                                {t('bots.table.paired', {defaultValue: 'paired'})}
                                            </Typography>
                                        )}
                                        {chat.disabled && (
                                            <Typography variant="caption" sx={{color: 'text.disabled', flexShrink: 0}}>
                                                {t('bots.table.disabled', {defaultValue: 'disabled'})}
                                            </Typography>
                                        )}
                                        <Tooltip title={chat.disabled
                                            ? t('bots.table.enableChat', {defaultValue: 'Enable — accept its messages again'})
                                            : t('bots.table.disableChat', {defaultValue: 'Disable — silently drop its messages'})}>
                                            <span>
                                                <IconButton
                                                    size="small"
                                                    onClick={() => handleToggleDisabled(chat)}
                                                    disabled={busyChat === chat.chat_id}
                                                    sx={{p: 0.25, flexShrink: 0}}
                                                    color={chat.disabled ? 'default' : 'warning'}
                                                    aria-label={chat.disabled
                                                        ? t('bots.table.enableChat', {defaultValue: 'Enable chat'})
                                                        : t('bots.table.disableChat', {defaultValue: 'Disable chat'})}
                                                >
                                                    <BlockIcon fontSize="inherit"/>
                                                </IconButton>
                                            </span>
                                        </Tooltip>
                                        <Tooltip title={t('bots.table.deleteChat', {defaultValue: 'Delete this chat record'})}>
                                            <span>
                                                <IconButton
                                                    size="small"
                                                    onClick={() => setDeleteTarget(chat.chat_id)}
                                                    disabled={busyChat === chat.chat_id}
                                                    sx={{p: 0.25, flexShrink: 0}}
                                                    color="error"
                                                    aria-label={t('bots.table.deleteChat', {defaultValue: 'Delete chat'})}
                                                >
                                                    <DeleteIcon fontSize="inherit"/>
                                                </IconButton>
                                            </span>
                                        </Tooltip>
                                    </Stack>
                                </ListItem>
                            ))}
                        </List>
                    )}
                    {!loading && !error && disabledCount > 0 && (
                        <Box sx={{mt: 0.5, textAlign: 'right'}}>
                            <Link
                                component="button"
                                variant="caption"
                                underline="hover"
                                onClick={() => setShowDisabled(v => !v)}
                                sx={{color: 'text.secondary'}}
                            >
                                {showDisabled
                                    ? t('bots.table.hideDisabled', {defaultValue: 'Hide disabled'})
                                    : t('bots.table.showDisabled', {defaultValue: 'Show disabled ({{count}})', count: disabledCount})}
                            </Link>
                        </Box>
                    )}
                </Box>
            </Popover>
            <Dialog open={deleteTarget !== null} onClose={() => setDeleteTarget(null)}>
                <DialogTitle>
                    {t('bots.table.deleteChatTitle', {defaultValue: 'Delete this chat?'})}
                </DialogTitle>
                <DialogContent>
                    <DialogContentText component="div">
                        <Typography variant="body2" sx={{fontFamily: 'monospace', mb: 1}}>{deleteTarget}</Typography>
                        <Typography variant="body2">
                            {t('bots.table.deleteChatBody', {
                                defaultValue: 'Its pairing, whitelist, and project binding are removed. If it messages the bot again it re-registers as a brand-new chat (re-pairing required when pairing is enforced). Session history is untouched. To block it instead, use Disable.',
                            })}
                        </Typography>
                    </DialogContentText>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setDeleteTarget(null)}>
                        {t('common.cancel', {defaultValue: 'Cancel'})}
                    </Button>
                    <Button color="error" onClick={() => deleteTarget && handleDelete(deleteTarget)}>
                        {t('common.delete', {defaultValue: 'Delete'})}
                    </Button>
                </DialogActions>
            </Dialog>
            <Tooltip
                title={t('bots.table.showChats', {defaultValue: 'Show reachable chats (copy Chat ID)'})}
                open={tooltipOpen && !open}
                onOpen={() => setTooltipOpen(true)}
                onClose={() => setTooltipOpen(false)}
                leaveDelay={0}
            >
                <IconButton
                    size="small"
                    color="primary"
                    onClick={handleOpen}
                    disabled={disabled}
                    aria-label={t('bots.table.showChats', {defaultValue: 'Show reachable chats'})}
                >
                    <MessageIcon fontSize="small"/>
                </IconButton>
            </Tooltip>
        </>
    );
};

export default BotChatsButton;
