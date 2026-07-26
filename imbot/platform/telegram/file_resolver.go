package telegram

import (
	"context"
	"fmt"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/tingly-dev/tingly-box/imbot/core"
)

// Telegram does not put attachments behind a public URL. An inbound message
// carries a file_id, which the adapter normalises to "tgfile://<file_id>"; a
// download link has to be fetched with getFile and is valid for about an hour.
//
// This is where that knowledge belongs — the same package that mints the
// scheme, and the one that already holds the bot token. It previously lived in
// internal/remote_control, which meant the application hand-built
// api.telegram.org URLs and kept a second copy of the token to do it.

// ResolveFileURL implements core.FileResolver.
func (b *Bot) ResolveFileURL(ctx context.Context, mediaURL string) (string, error) {
	fileID, ok := strings.CutPrefix(mediaURL, string(core.FileURLSchemeTelegram)+"://")
	if !ok {
		// Not ours; hand it back untouched.
		return mediaURL, nil
	}
	if b.api == nil {
		return "", core.NewBotError(core.ErrNotSupported, "telegram bot is not connected", false)
	}

	file, err := b.api.GetFile(ctx, &tgbot.GetFileParams{FileID: fileID})
	if err != nil {
		return "", fmt.Errorf("telegram getFile %s: %w", fileID, err)
	}
	// FileDownloadLink builds the token-bearing URL, so the token stays inside
	// this package.
	return b.api.FileDownloadLink(file), nil
}
