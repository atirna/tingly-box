package webproxy

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	coretool "github.com/tingly-dev/tingly-box/internal/tool"
)

// Bare tool names, before namespacing. These are the names the web proxy's own
// executor dispatches on.
const (
	ToolWebSearch = "web_search"
	ToolWebFetch  = "web_fetch"
)

// Namespaced tool names as they appear in the outbound request and in the
// model's tool calls. The normalized `tingly_box_mcp__<source>__<tool>` scheme
// is borrowed (see coretool.WebProxySourceID) so these tools travel through the
// existing server-side tool loop, which executes them in-process and never
// surfaces them to the client.
var (
	NameWebSearch = coretool.NormalizeToolName(coretool.WebProxySourceID, ToolWebSearch)
	NameWebFetch  = coretool.NormalizeToolName(coretool.WebProxySourceID, ToolWebFetch)
)

// IsWebProxyTool reports whether a normalized tool name belongs to the web
// proxy. Used by the tool loop to route execution here instead of into the MCP
// runtime, and by the dispatch gates to decide whether a request needs the
// loop at all.
func IsWebProxyTool(normalizedName string) bool {
	sourceID, _, ok := coretool.ParseNormalizedToolName(normalizedName)
	return ok && sourceID == coretool.WebProxySourceID
}

// BareToolName strips the namespace from a web proxy tool name, returning the
// bare name ("web_search" / "web_fetch") and whether the name belonged to this
// package.
func BareToolName(normalizedName string) (string, bool) {
	sourceID, bare, ok := coretool.ParseNormalizedToolName(normalizedName)
	if !ok || sourceID != coretool.WebProxySourceID {
		return "", false
	}
	return bare, true
}

// InjectedTools returns the two function tools the web proxy offers to the
// downstream model, in the canonical OpenAI shape. The Anthropic shapes are
// derived from these by the shared tool converter, so there is a single
// definition of the schemas.
//
// The descriptions deliberately read like ordinary web tools: the downstream
// model must not need to know that a second model is doing the work.
func InjectedTools() []openai.ChatCompletionToolUnionParam {
	return []openai.ChatCompletionToolUnionParam{
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        NameWebSearch,
			Description: openai.String("Search the public web and return a summary of the most relevant results with their source URLs. Use this whenever the answer depends on information that may be newer than your training data, or on facts you cannot verify from the conversation alone."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The search query, phrased as you would type it into a search engine.",
					},
					"allowed_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional. Restrict results to these domains.",
					},
					"blocked_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional. Exclude results from these domains.",
					},
				},
				"required": []string{"query"},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        NameWebFetch,
			Description: openai.String("Fetch a single web page by URL and return its content as text. Use this when you already know the exact page you need to read."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The absolute URL of the page to fetch.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Optional. What to extract or focus on within the page.",
					},
				},
				"required": []string{"url"},
			},
		}),
	}
}

// searchArgs / fetchArgs mirror the schemas above. Arguments arrive as the raw
// JSON string the model produced, so every field is treated as best-effort:
// a malformed payload degrades to a missing-argument error rather than a panic.
type searchArgs struct {
	Query          string   `json:"query"`
	AllowedDomains []string `json:"allowed_domains"`
	BlockedDomains []string `json:"blocked_domains"`
}

type fetchArgs struct {
	URL    string `json:"url"`
	Prompt string `json:"prompt"`
}

func parseSearchArgs(raw string) (searchArgs, bool) {
	var a searchArgs
	if strings.TrimSpace(raw) == "" {
		return a, false
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return a, false
	}
	a.Query = strings.TrimSpace(a.Query)
	return a, a.Query != ""
}

func parseFetchArgs(raw string) (fetchArgs, bool) {
	var a fetchArgs
	if strings.TrimSpace(raw) == "" {
		return a, false
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return a, false
	}
	a.URL = strings.TrimSpace(a.URL)
	a.Prompt = strings.TrimSpace(a.Prompt)
	return a, a.URL != ""
}
