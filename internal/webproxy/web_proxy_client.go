package webproxy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"

	"github.com/tingly-dev/tingly-box/internal/client"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/assembler"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// providerResolver is the subset of routing.ProviderResolver this package
// needs. Defined locally so this package does not depend on internal/routing.
type providerResolver interface {
	GetProviderByUUID(uuid string) (*typ.Provider, error)
}

// WebClient is the dependency Service needs to borrow web access. The real
// adapter dispatches through client.ClientPool based on the borrowed
// provider's APIStyle; tests substitute a fake.
//
// Both methods return the borrowed model's answer as plain text — the web
// proxy hands that text straight back to the downstream model as a tool
// result. Returning ("", nil) means "nothing found"; a non-nil error is
// surfaced to the downstream model as a tool error.
type WebClient interface {
	Search(ctx context.Context, service *loadbalance.Service, args SearchRequest) (string, error)
	Fetch(ctx context.Context, service *loadbalance.Service, args FetchRequest) (string, error)
}

// SearchRequest / FetchRequest are the normalized arguments extracted from the
// downstream model's tool call.
type SearchRequest struct {
	Query          string
	AllowedDomains []string
	BlockedDomains []string
}

type FetchRequest struct {
	URL    string
	Prompt string
}

const (
	// defaultWebMaxTokens bounds the borrowed model's answer. Search results
	// and page extracts are fed back into the downstream model's context, so
	// the budget is generous but not unbounded.
	defaultWebMaxTokens = 2048

	// searchInstruction / fetchInstruction frame the borrowed call. The
	// borrowed model is being used as a research tool, not as a chat partner:
	// the instruction asks for source-attributed findings and nothing else, so
	// the text can be handed to the downstream model verbatim.
	searchInstruction = "Search the web and report what you find. Answer with the findings only — no preamble, no offer to help further. Include the source URL next to each fact."
	fetchInstruction  = "Fetch the page at the URL below and report its content as plain text. Answer with the content only — no preamble."
)

// poolWebClient is the production WebClient. Each call resolves the borrowed
// provider and dispatches by APIStyle:
//
//   - Anthropic-style providers get the native server tools
//     (web_search_20250305 / web_fetch_20250910) attached, so the provider
//     performs the search itself and the model reports the result.
//   - OpenAI-style providers are called as a plain chat completion. OpenAI
//     Chat has no portable native web tool, so this path relies on the
//     borrowed model having built-in web access (Perplexity Sonar,
//     *-search-preview, grounded gateway models). A model without it will
//     answer from memory — configure an Anthropic-style service when the
//     distinction matters.
//
// Any other API style is an error, which the executor turns into a tool error
// the downstream model can see and react to.
type poolWebClient struct {
	pool     *client.ClientPool
	resolver providerResolver
}

// NewPoolWebClient builds the production web client backed by the shared SDK
// pool. resolver is typically the server config (routing.ProviderResolver).
func NewPoolWebClient(pool *client.ClientPool, resolver providerResolver) WebClient {
	return &poolWebClient{pool: pool, resolver: resolver}
}

func (a *poolWebClient) Search(ctx context.Context, service *loadbalance.Service, args SearchRequest) (string, error) {
	provider, err := a.provider(service)
	if err != nil {
		return "", err
	}

	prompt := searchInstruction + "\n\nQuery: " + args.Query
	if len(args.AllowedDomains) > 0 {
		prompt += "\nOnly use sources from these domains: " + strings.Join(args.AllowedDomains, ", ")
	}
	if len(args.BlockedDomains) > 0 {
		prompt += "\nDo not use sources from these domains: " + strings.Join(args.BlockedDomains, ", ")
	}

	switch provider.APIStyle {
	case protocol.APIStyleAnthropic:
		return a.viaAnthropic(ctx, provider, service.Model, prompt, anthropic.BetaToolUnionParam{
			OfWebSearchTool20250305: &anthropic.BetaWebSearchTool20250305Param{
				AllowedDomains: args.AllowedDomains,
				BlockedDomains: args.BlockedDomains,
			},
		})
	case protocol.APIStyleOpenAI:
		return a.viaOpenAI(ctx, provider, service.Model, prompt)
	default:
		return "", fmt.Errorf("web proxy: api_style %q not supported", provider.APIStyle)
	}
}

func (a *poolWebClient) Fetch(ctx context.Context, service *loadbalance.Service, args FetchRequest) (string, error) {
	provider, err := a.provider(service)
	if err != nil {
		return "", err
	}

	prompt := fetchInstruction + "\n\nURL: " + args.URL
	if args.Prompt != "" {
		prompt += "\nFocus on: " + args.Prompt
	}

	switch provider.APIStyle {
	case protocol.APIStyleAnthropic:
		return a.viaAnthropic(ctx, provider, service.Model, prompt, anthropic.BetaToolUnionParam{
			OfWebFetchTool20250910: &anthropic.BetaWebFetchTool20250910Param{},
		})
	case protocol.APIStyleOpenAI:
		return a.viaOpenAI(ctx, provider, service.Model, prompt)
	default:
		return "", fmt.Errorf("web proxy: api_style %q not supported", provider.APIStyle)
	}
}

func (a *poolWebClient) provider(service *loadbalance.Service) (*typ.Provider, error) {
	if service == nil {
		return nil, errors.New("web proxy: nil service")
	}
	if a.pool == nil || a.resolver == nil {
		return nil, errors.New("web proxy: pool or resolver not configured")
	}
	provider, err := a.resolver.GetProviderByUUID(service.Provider)
	if err != nil || provider == nil {
		return nil, fmt.Errorf("web proxy: resolve provider %q: %w", service.Provider, err)
	}
	return provider, nil
}

// viaAnthropic runs one turn against an Anthropic-style provider with the given
// native server tool attached. Streaming is used for the same reason the vision
// proxy uses it: server-tool turns are long-running and several
// Anthropic-compatible gateways only implement the streaming endpoint for them.
// The shared assembler folds the events back into a single message.
func (a *poolWebClient) viaAnthropic(ctx context.Context, provider *typ.Provider, model, prompt string, tool anthropic.BetaToolUnionParam) (string, error) {
	c := a.pool.GetAnthropicClient(ctx, provider, model)
	if c == nil {
		return "", errors.New("web proxy: pool returned nil anthropic client")
	}

	stream := c.BetaMessagesNewStreaming(ctx, &anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: defaultWebMaxTokens,
		Tools:     []anthropic.BetaToolUnionParam{tool},
		Messages: []anthropic.BetaMessageParam{{
			Role: anthropic.BetaMessageParamRoleUser,
			Content: []anthropic.BetaContentBlockParamUnion{
				{OfText: &anthropic.BetaTextBlockParam{Text: prompt}},
			},
		}},
	})
	defer stream.Close()

	asm := assembler.NewAnthropicBetaSDKAssembler()
	for stream.Next() {
		if err := asm.Accumulate(stream.Current()); err != nil {
			return "", fmt.Errorf("web proxy: accumulate beta stream: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}

	msg := asm.Finish()
	var sb strings.Builder
	for _, b := range msg.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// viaOpenAI runs one plain chat completion against an OpenAI-style provider.
// Non-streaming: unlike the Anthropic server-tool turn there is no long
// tool-execution phase to keep alive, and the whole answer is needed before it
// can be handed back as a tool result anyway.
func (a *poolWebClient) viaOpenAI(ctx context.Context, provider *typ.Provider, model, prompt string) (string, error) {
	c := a.pool.GetOpenAIClient(ctx, provider, model)
	if c == nil {
		return "", errors.New("web proxy: pool returned nil openai client")
	}

	resp, err := c.ChatCompletionsNew(ctx, openai.ChatCompletionNewParams{
		Model:     openai.ChatModel(model),
		MaxTokens: openai.Int(defaultWebMaxTokens),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	for _, ch := range resp.Choices {
		if text := strings.TrimSpace(ch.Message.Content); text != "" {
			return text, nil
		}
	}
	return "", nil
}
