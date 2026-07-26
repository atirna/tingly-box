package core

import "context"

// A platform that cannot hand out a plain download URL for an attachment
// instead mints a scheme of its own — Telegram's "tgfile://<file_id>" — and
// something has to turn that back into something fetchable.
//
// That something used to be the application: internal/remote_control's file
// store knew the tgfile:// scheme, knew Telegram's getFile endpoint, and built
// api.telegram.org URLs with the bot token embedded in the path. Three pieces
// of Telegram knowledge in a package that is supposed to be
// platform-agnostic, and a token copied out of settings into a second place
// that had to be kept in sync.
//
// The platform that minted the URL is the one that can resolve it, and it
// already holds the credential.

// FileResolver turns a platform-minted media URL into one an HTTP client can
// fetch. Platforms whose attachments already carry usable URLs do not
// implement it.
type FileResolver interface {
	// ResolveFileURL returns a fetchable URL for mediaURL. A URL this platform
	// does not own is returned unchanged rather than rejected, so callers can
	// pass everything through without inspecting schemes.
	ResolveFileURL(ctx context.Context, mediaURL string) (string, error)
}

// AsFileResolver reports whether a bot can resolve its own media URLs.
//
// Interface assertion, not a concrete type assertion: the previous generation
// of this pattern asserted on *telegram.Bot and so could never be satisfied by
// any other platform, however capable.
func AsFileResolver(bot Bot) (FileResolver, bool) {
	resolver, ok := bot.(FileResolver)
	return resolver, ok
}

// ResolveFileURL resolves a media URL through the bot that produced it,
// returning the URL unchanged when the platform has nothing to resolve.
func ResolveFileURL(ctx context.Context, bot Bot, mediaURL string) (string, error) {
	resolver, ok := AsFileResolver(bot)
	if !ok {
		return mediaURL, nil
	}
	return resolver.ResolveFileURL(ctx, mediaURL)
}
