package protocol

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"google.golang.org/genai"
)

// UpstreamStatus extracts the HTTP status code that an upstream provider
// returned, so the gateway can propagate it to the client instead of flattening
// every forwarding failure into a 500. It understands the error types returned
// by each vendor SDK (OpenAI / Anthropic share apierror.Error; google-genai
// uses genai.APIError). When the error does not carry a usable upstream status
// (e.g. a transport-level failure with no HTTP response), it returns fallback.
func UpstreamStatus(err error, fallback int) int {
	if err == nil {
		return fallback
	}

	var oaiErr *openai.Error
	if errors.As(err, &oaiErr) && oaiErr.StatusCode >= 400 {
		return oaiErr.StatusCode
	}

	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) && anthropicErr.StatusCode >= 400 {
		return anthropicErr.StatusCode
	}

	var genaiErr genai.APIError
	if errors.As(err, &genaiErr) && genaiErr.Code >= 400 {
		return genaiErr.Code
	}

	return fallback
}

// UpstreamErrorMessage returns err.Error(), recovering the upstream response
// body when the SDK's own message lost it. The OpenAI/Anthropic SDKs
// stringify only the body's top-level "error" key into Error(); a 4xx whose
// body has any other shape — plain text, {"message": ...}, an HTML error
// page, or an empty body — prints as a bare "POST <url>: 400 Bad Request"
// with the actual diagnostic dropped. The SDK does re-populate Response.Body
// with the full contents for debugging, so read it back and append a bounded
// snippet. When Error() already carries the body (RawJSON non-empty), the
// message is returned unchanged.
func UpstreamErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	var body string
	var oaiErr *openai.Error
	var anthErr *anthropic.Error
	switch {
	case errors.As(err, &oaiErr) && oaiErr.RawJSON() == "":
		body = readRepopulatedBody(oaiErr.Response)
	case errors.As(err, &anthErr) && anthErr.RawJSON() == "":
		body = readRepopulatedBody(anthErr.Response)
	}
	if body == "" {
		return msg
	}
	const maxBody = 512
	if len(body) > maxBody {
		body = body[:maxBody] + "…(truncated)"
	}
	return strings.TrimSpace(msg) + " — upstream body: " + body
}

// readRepopulatedBody reads the response body the SDK re-populated after a
// >=400 status and puts it back so later readers (DumpResponse) still work.
// Returns "" for a nil/unreadable/empty body.
func readRepopulatedBody(res *http.Response) string {
	if res == nil || res.Body == nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(b))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
