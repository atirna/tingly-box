package stream

import (
	"context"
	"errors"

	openaistream "github.com/openai/openai-go/v3/packages/ssestream"

	"github.com/tingly-dev/tingly-box/internal/protocol"
)

// salvageableStreamErr reports whether a stream ending is the kind the
// salvage_truncated_stream flag may close gracefully: a clean EOF (nil) or a
// transport-level read error (connection reset, unexpected EOF — an upstream
// that cut the connection mid-generation). Two endings are never salvaged:
//
//   - an in-band SSE error payload (openaistream.StreamError) — the upstream
//     explicitly reported an error, papering over it would hide real failures;
//   - client-side cancellation — the client is gone, there is nobody to send
//     a synthesized completion to.
func salvageableStreamErr(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) || protocol.IsContextCanceled(err) {
		return false
	}
	var inBand *openaistream.StreamError
	return !errors.As(err, &inBand)
}
