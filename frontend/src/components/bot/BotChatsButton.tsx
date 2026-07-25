import {Message as MessageIcon, ContentCopy as CopyIcon} from '@/components/icons';
import type {BotChat} from '@/types/bot';
import {api} from '@/services/api';
import {notify} from '@/utils/notify';
import {
    Box,
    CircularProgress,
    IconButton,
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
interface BotChatsButtonProps {
    botUUID: string;
    disabled?: boolean;
}

const BotChatsButton: React.FC<BotChatsButtonProps> = ({botUUID, disabled}) => {
    const {t} = useTranslation();
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    const [tooltipOpen, setTooltipOpen] = useState(false);
    const [loading, setLoading] = useState(false);
    const [chats, setChats] = useState<BotChat[]>([]);
    const [error, setError] = useState<string | null>(null);

    const handleOpen = useCallback(async (e: React.MouseEvent<HTMLElement>) => {
        // Close the tooltip the moment the popover opens, otherwise its hover
        // text lingers over the open popover.
        setTooltipOpen(false);
        setAnchor(e.currentTarget);
        if (chats.length || error) return; // already loaded this session
        setLoading(true);
        setError(null);
        const result = await api.listBotChats(botUUID);
        setLoading(false);
        if (result.error) {
            setError(result.error);
        } else {
            setChats(result.chats ?? []);
        }
    }, [botUUID, chats.length, error]);

    const handleClose = useCallback(() => setAnchor(null), []);

    const handleCopy = useCallback(async (chatID: string) => {
        try {
            await navigator.clipboard.writeText(chatID);
            notify.success(t('bots.table.chatIdCopied', {defaultValue: 'Chat ID copied'}));
        } catch {
            notify.error(t('bots.table.chatIdCopyFailed', {defaultValue: 'Copy failed — check clipboard permissions'}));
        }
    }, [t]);

    const open = Boolean(anchor);

    return (
        <>
            <Popover
                open={open}
                anchorEl={anchor}
                onClose={handleClose}
                anchorOrigin={{vertical: 'bottom', horizontal: 'left'}}
                transformOrigin={{vertical: 'top', horizontal: 'left'}}
                slotProps={{paper: {sx: {minWidth: 280, maxWidth: 380, mt: 0.5}}}}
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
                    {!loading && !error && chats.length === 0 && (
                        <Typography variant="body2" sx={{color: 'text.disabled', py: 1}}>
                            {t('bots.table.noChats', {defaultValue: 'No chats yet — message the bot once to register one.'})}
                        </Typography>
                    )}
                    {!loading && !error && chats.length > 0 && (
                        <List dense disablePadding sx={{mt: 0.5}}>
                            {chats.map((chat) => (
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
                                            }}
                                        >
                                            {chat.chat_id}
                                        </Typography>
                                        {chat.is_paired && (
                                            <Typography variant="caption" sx={{color: 'success.main', flexShrink: 0}}>
                                                {t('bots.table.paired', {defaultValue: 'paired'})}
                                            </Typography>
                                        )}
                                    </Stack>
                                </ListItem>
                            ))}
                        </List>
                    )}
                </Box>
            </Popover>
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
