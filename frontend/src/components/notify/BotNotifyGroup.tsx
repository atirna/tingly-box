import {Send as SendIcon, ContentCopy as CopyIcon} from '@/components/icons';
import {api} from '@/services/api';
import {notify} from '@/utils/notify';
import {isPairingRequired} from '@/types/bot';
import type {BotChat, BotSettings} from '@/types/bot';
import {fontMono} from '@/theme/fonts';
import NotifyTestDialog from '@/components/notify/NotifyTestDialog';
import {
    Box,
    Chip,
    CircularProgress,
    IconButton,
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
} from '@mui/material';
import {useCallback, useEffect, useState} from 'react';
import {useTranslation} from 'react-i18next';

// BotNotifyGroup is one bot's panel on the IM Notify page: a header (name +
// platform + the enabled switch that governs whether this bot can be driven)
// over an ALWAYS-EXPANDED table of the chats it can reach. Each chat row shows
// the concrete value the operator needs — the channel-native chat_id that
// POST /api/v1/bots/:bot/notify takes in its body — with copy + send-test
// inline, so there is no extra click to reach the work surface (ux-principles
// #1 organize IA around the user's question, #5 show the concrete value, #11
// hand over the next action's artifact).
//
// The chats are fetched eagerly when the bot is enabled, not behind a button:
// "what can I send to?" is the page's whole point, so the answer is on screen.
// A disabled bot has no channel in the registry (404), so we don't fetch then.
export interface BotNotifyGroupProps {
    bot: BotSettings;
    onToggle: (uuid: string) => void;
    isToggling?: boolean;
}

// Em-dash placeholder for empty status/project/updated cells — shared so the
// three columns stay visually consistent (one place to tweak the styling).
const Dash: React.FC = () => (
    <Typography variant="body2" sx={{color: 'text.secondary'}}>—</Typography>
);

const BotNotifyGroup: React.FC<BotNotifyGroupProps> = ({bot, onToggle, isToggling}) => {
    const {t} = useTranslation();
    const enabled = bot.enabled ?? true;

    const [chats, setChats] = useState<BotChat[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [testChatID, setTestChatID] = useState<string | null>(null);

    const loadChats = useCallback(async () => {
        if (!bot.uuid) return;
        setLoading(true);
        setError(null);
        const result = await api.listBotChats(bot.uuid);
        setLoading(false);
        if (result.error) {
            setError(result.error);
        } else {
            setChats(result.chats ?? []);
        }
    }, [bot.uuid]);

    // Eager-load only when the bot is enabled (a stopped bot 404s). Re-fetch on
    // enable transitions so toggling on surfaces fresh chats.
    useEffect(() => {
        if (enabled) loadChats();
        else {
            setChats([]);
            setError(null);
        }
    }, [enabled, loadChats]);

    const handleCopy = useCallback(async (chatID: string) => {
        try {
            await navigator.clipboard.writeText(chatID);
            notify.success(t('notify.chat.copied', {defaultValue: 'Chat ID copied'}));
        } catch {
            notify.error(t('notify.chat.copyFailed', {defaultValue: 'Copy failed — check clipboard permissions'}));
        }
    }, [t]);

    const openTest = useCallback((chatID: string) => setTestChatID(chatID), []);
    const closeTest = useCallback(() => setTestChatID(null), []);

    return (
        <Box
            sx={{
                border: '1px solid',
                borderColor: 'divider',
                borderRadius: 1.5,
                overflow: 'hidden',
            }}
        >
            {/* Header: name + platform + enabled switch (the on/off for driving
                this bot) + chat count. The switch is the bot's existing enabled
                flag — surfaced here because "can I use this bot to notify?" is
                exactly the question this page answers. */}
            <Box
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1.5,
                    px: 2,
                    py: 1.25,
                    bgcolor: 'action.hover',
                    flexWrap: 'wrap',
                }}
            >
                {/* Fixed-width name column so every group's name chip aligns
                    across rows — name length varies, but the column shouldn't. */}
                <Tooltip title={bot.name || bot.platform}>
                    <Typography noWrap variant="body2" sx={{fontWeight: 600, flexShrink: 0, width: {xs: 96, sm: 150}}}>
                        {bot.name || bot.platform}
                    </Typography>
                </Tooltip>
                <Chip label={bot.platform} size="small" />
                <Box sx={{flexGrow: 1}} />
                {enabled && !loading && (
                    <Typography variant="body2" sx={{color: 'text.secondary'}}>
                        {chats.length > 0
                            ? t('notify.group.chatCount', {defaultValue: '{{count}} reachable chat(s)', count: chats.length})
                            : t('notify.group.noChats', {defaultValue: 'No reachable chats'})}
                    </Typography>
                )}
                <Tooltip title={enabled
                    ? t('notify.group.disableHint', {defaultValue: 'Disable this bot'})
                    : t('notify.group.enableHint', {defaultValue: 'Enable this bot to send notifications'})}>
                    {/* The switch is wired to the same toggle the Bots table uses
                        (POST /imbot-settings/:uuid/toggle) — it starts/stops the
                        bot's channel, which is what governs whether notify can
                        reach it. A disabled-bot row shows why chats are absent. */}
                    <Stack
                        direction="row"
                        spacing={0.75}
                        sx={{alignItems: 'center', cursor: isToggling ? 'wait' : 'pointer'}}
                    >
                        <Switch
                            size="small"
                            color="success"
                            checked={enabled}
                            disabled={isToggling}
                            onChange={() => onToggle(bot.uuid!)}
                        />
                        {isToggling ? (
                            <CircularProgress size={14} />
                        ) : (
                            <Typography variant="body2" sx={{color: enabled ? 'success.main' : 'text.secondary', fontWeight: 600}}>
                                {enabled ? t('common.on', {defaultValue: 'On'}) : t('common.off', {defaultValue: 'Off'})}
                            </Typography>
                        )}
                    </Stack>
                </Tooltip>
            </Box>

            {/* Body: the reachable chats, always expanded (no extra click). */}
            <Box sx={{px: {xs: 1, sm: 2}, py: 1.5}}>
                {!enabled ? (
                    <Typography variant="body2" sx={{color: 'text.disabled', py: 1}}>
                        {t('notify.group.disabledBody', {defaultValue: 'Bot is off — enable it to see and send to its reachable chats.'})}
                    </Typography>
                ) : loading ? (
                    <Box sx={{display: 'flex', justifyContent: 'center', py: 2}}>
                        <CircularProgress size={20} />
                    </Box>
                ) : error ? (
                    <Typography variant="body2" sx={{color: 'error.main', py: 1}}>{error}</Typography>
                ) : chats.length === 0 ? (
                    <Typography variant="body2" sx={{color: 'text.disabled', py: 1}}>
                        {isPairingRequired(bot)
                            ? t('notify.group.emptyPairFirst', {defaultValue: 'No chats yet. Pair this bot, then send it a message on {{platform}} — its Chat ID appears here.', platform: bot.platform || 'its platform'})
                            : t('notify.group.empty', {defaultValue: 'No chats yet. Send any message to this bot on {{platform}} and its Chat ID appears here.', platform: bot.platform || 'its platform'})}
                    </Typography>
                ) : (
                    <TableContainer sx={{overflowX: 'auto'}}>
                        <Table size="small" sx={{tableLayout: 'fixed'}}>
                            <TableHead>
                                <TableRow sx={{'& .MuiTableCell-head': {color: 'text.primary'}}}>
                                    <TableCell sx={{fontWeight: 600, width: '45%'}}>{t('notify.group.colChatId', {defaultValue: 'Chat ID'})}</TableCell>
                                    <TableCell sx={{fontWeight: 600, width: 90}}>{t('notify.group.colStatus', {defaultValue: 'Status'})}</TableCell>
                                    <TableCell sx={{fontWeight: 600, width: '25%'}}>{t('notify.group.colProject', {defaultValue: 'Project'})}</TableCell>
                                    <TableCell sx={{fontWeight: 600, width: 130}}>{t('notify.group.colUpdated', {defaultValue: 'Updated'})}</TableCell>
                                    <TableCell sx={{fontWeight: 600, width: 90, textAlign: 'right'}}>{t('notify.group.colActions', {defaultValue: 'Actions'})}</TableCell>
                                </TableRow>
                            </TableHead>
                            <TableBody>
                                {chats.map((chat) => (
                                    <TableRow key={chat.chat_id} hover>
                                        <TableCell sx={{verticalAlign: 'middle'}}>
                                            <Typography variant="body2" component="span" sx={{fontFamily: fontMono, color: 'text.primary'}}>
                                                {chat.chat_id}
                                            </Typography>
                                        </TableCell>
                                        <TableCell sx={{verticalAlign: 'middle'}}>
                                            {chat.is_paired ? (
                                                <Chip label={t('notify.group.paired', {defaultValue: 'paired'})} size="small" color="success" variant="outlined" />
                                            ) : (
                                                <Dash/>
                                            )}
                                        </TableCell>
                                        <TableCell sx={{verticalAlign: 'middle'}}>
                                            {chat.project_path ? (
                                                <Tooltip title={chat.project_path}>
                                                    <Typography variant="body2" component="span" noWrap sx={{display: 'block', color: 'text.secondary'}}>
                                                        {chat.project_path}
                                                    </Typography>
                                                </Tooltip>
                                            ) : (
                                                <Dash/>
                                            )}
                                        </TableCell>
                                        <TableCell sx={{verticalAlign: 'middle'}}>
                                            {chat.updated_at ? (
                                                <Typography variant="body2" sx={{color: 'text.secondary'}}>
                                                    {new Date(chat.updated_at).toLocaleString()}
                                                </Typography>
                                            ) : (
                                                <Dash/>
                                            )}
                                        </TableCell>
                                        <TableCell sx={{verticalAlign: 'middle'}}>
                                            <Stack direction="row" spacing={0.5} sx={{justifyContent: 'flex-end'}}>
                                                <Tooltip title={t('notify.group.copyChatId', {defaultValue: 'Copy Chat ID'})}>
                                                    <IconButton size="small" onClick={() => handleCopy(chat.chat_id)}>
                                                        <CopyIcon fontSize="small" />
                                                    </IconButton>
                                                </Tooltip>
                                                <Tooltip title={t('notify.group.sendToChat', {defaultValue: 'Send a test notification to this chat'})}>
                                                    <IconButton size="small" color="primary" onClick={() => openTest(chat.chat_id)}>
                                                        <SendIcon fontSize="small" />
                                                    </IconButton>
                                                </Tooltip>
                                            </Stack>
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </TableContainer>
                )}
            </Box>

            <NotifyTestDialog
                open={testChatID !== null}
                botUUID={bot.uuid!}
                botName={bot.name || bot.platform}
                chats={chats}
                initialChatID={testChatID ?? undefined}
                onClose={closeTest}
            />
        </Box>
    );
};

export default BotNotifyGroup;
