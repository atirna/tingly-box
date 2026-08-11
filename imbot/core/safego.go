package core

import (
	"fmt"
	"runtime/debug"
)

// Panic containment at the platform SDK trust boundary. recover() cannot
// cross goroutine boundaries, so it lives where the risk lives: entry points
// SDKs invoke on their own goroutines (RecoverCallback) and our receive
// loops (RecoverLoop). Rationale and the close-vs-reconnect split: ErrPanic
// in types.go and tingly-box .design/bot-panic-isolation.md.
//
// The bot's identity (platform, UUID) is stamped onto the log here, from the
// bot itself — call sites pass only the local role ("message", "poll loop")
// and cannot mislabel which bot crashed.

// logPanic is the shared log shape for contained panics. It must be called
// from the deferred Recover* function itself (recover() has already run).
func (b *BaseBot) logPanic(name, disposition string, r any) {
	b.Logger().Error("[%s/%s] panic in %s (contained, %s): %v\n%s",
		b.config.Platform, b.UUID(), name, disposition, r, debug.Stack())
}

// RecoverCallback contains a panic in a handler or SDK-invoked callback: the
// one event is dropped, the connection is left alone.
func (b *BaseBot) RecoverCallback(name string) {
	if r := recover(); r != nil {
		b.logPanic(name, "event dropped", r)
	}
}

// RecoverLoop contains a panic in a platform receive loop: the bot flips to
// disconnected and emits ErrPanic (see that const — close-and-rebuild, not
// reconnect-in-place, so deliberately no disconnect event). Register cleanup
// defers (wg.Done, close) BEFORE this one so they still run on panic.
func (b *BaseBot) RecoverLoop(name string) {
	if r := recover(); r != nil {
		b.logPanic(name, "closing bot", r)
		// State down WITHOUT the disconnect event: disconnect means
		// "reconnect in place", wrong for a bot whose state is suspect.
		b.UpdateConnected(false)
		b.UpdateReady(false)
		b.EmitError(NewPanicError(b.config.Platform, fmt.Sprintf("panic in %s: %v", name, r)))
	}
}
