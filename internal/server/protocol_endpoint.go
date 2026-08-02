package server

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// IncomingAPIType describes which OpenAI-style endpoint the client originally
// hit on this gateway. The resolver prefers the upstream endpoint that
// natively matches it whenever the provider declares support for that
// endpoint; otherwise the request is converted to a declared endpoint.
type IncomingAPIType string

const (
	IncomingAPIChat      IncomingAPIType = "chat"
	IncomingAPIResponses IncomingAPIType = "responses"
)

// ResolveOpenAIEndpoint picks an OpenAI upstream endpoint. The provider's
// OpenAIEndpoints declaration carries only capability facts; the selection
// policy is written out here, in one place:
//
//  1. Rule flag (flags.OpenAIEndpointOverride) → honored unconditionally.
//     When it conflicts with the declaration, a warning is logged but the
//     override wins (explicit routing control for debugging/special cases).
//  2. Native-first: if the provider declares the endpoint matching the
//     incoming request, use it (passthrough, no conversion).
//  3. Convert otherwise: if the provider declares only the other endpoint,
//     route there and convert the request.
//  4. Undeclared (empty) → Chat, the conservative ecosystem default: most
//     OpenAI-compatible vendors implement only /chat/completions. Providers
//     that genuinely support Responses must declare it (template prefill,
//     OAuth issuer, or the provider-edit checkbox).
//
// When an incoming Responses request routes to Chat, Responses-only fields
// (previous_response_id, include, background, truncation, reasoning) are
// silently dropped by ConvertOpenAIResponsesToChat — the same posture as
// Anthropic→Chat downgrades. The user accepts this via the declaration.
//
// Pure function: no Server state, no probe lookups, no I/O.
func ResolveOpenAIEndpoint(provider *typ.Provider, flags typ.RuleFlags, incoming IncomingAPIType) (protocol.APIType, error) {
	if provider == nil {
		return "", fmt.Errorf("provider is required for endpoint selection")
	}

	// Rule override takes first priority (per design intent from .design/openai-endpoint-routing.md)
	switch ParseEndpointOverride(flags.OpenAIEndpointOverride) {
	case OverrideChat:
		if provider.SupportsOpenAIEndpoint(ai.OpenAIEndpointResponses) && !provider.SupportsOpenAIEndpoint(ai.OpenAIEndpointChat) {
			logrus.Warnf("Rule forces chat endpoint on responses-only provider %s", provider.UUID)
		}
		return protocol.TypeOpenAIChat, nil

	case OverrideResponses:
		if !provider.SupportsOpenAIEndpoint(ai.OpenAIEndpointResponses) {
			logrus.Warnf("Rule forces responses endpoint on provider %s which does not declare it", provider.UUID)
		}
		return protocol.TypeOpenAIResponses, nil
	}

	// Native-first: mirror the incoming endpoint when the provider declares it.
	native := ai.OpenAIEndpointChat
	if incoming == IncomingAPIResponses {
		native = ai.OpenAIEndpointResponses
	}
	if provider.SupportsOpenAIEndpoint(native) {
		return openAIEndpointToAPIType(native), nil
	}

	// Convert to the declared endpoint when the native one is missing.
	if native != ai.OpenAIEndpointChat && provider.SupportsOpenAIEndpoint(ai.OpenAIEndpointChat) {
		return protocol.TypeOpenAIChat, nil
	}
	if native != ai.OpenAIEndpointResponses && provider.SupportsOpenAIEndpoint(ai.OpenAIEndpointResponses) {
		return protocol.TypeOpenAIResponses, nil
	}

	// Undeclared: conservative ecosystem default.
	return protocol.TypeOpenAIChat, nil
}

// openAIEndpointToAPIType maps a declared endpoint to the protocol APIType
// the dispatch layer consumes.
func openAIEndpointToAPIType(e ai.OpenAIEndpoint) protocol.APIType {
	if e == ai.OpenAIEndpointResponses {
		return protocol.TypeOpenAIResponses
	}
	return protocol.TypeOpenAIChat
}

// EndpointOverride is the typed value of the openai_endpoint_override rule
// flag. It forces an OpenAI request onto a specific endpoint, overriding
// the provider's declared OpenAIEndpoints default (see
// ResolveOpenAIEndpoint).
type EndpointOverride string

const (
	OverrideAuto      EndpointOverride = "auto"
	OverrideChat      EndpointOverride = "chat"
	OverrideResponses EndpointOverride = "responses"
)

// ParseEndpointOverride coerces a raw rule-flag string to a known
// EndpointOverride. Empty, "auto" and any unrecognized value map to
// OverrideAuto so misconfigured rules degrade safely.
func ParseEndpointOverride(s string) EndpointOverride {
	switch s {
	case string(OverrideChat):
		return OverrideChat
	case string(OverrideResponses):
		return OverrideResponses
	default:
		return OverrideAuto
	}
}
