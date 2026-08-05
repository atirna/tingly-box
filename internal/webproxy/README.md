# webproxy

The web proxy plugin: when the downstream model has no web access, borrow the
web access of a configured `{provider, model}` pair.

See `.design/web-proxy.md` for the product-level design (scopes, configuration
matrix, data model, relationship to the MCP webtools). This README covers the
implementation — the two halves and how they meet.

## Wiring

```
boot (internal/server/server.go)
  └─► webproxy.NewServiceFromPool(pool, resolver)
        └─► Service{ Client: poolWebClient{pool, resolver} }

per request — request half
  ResolveRuleFlagsWithScenario (internal/protocolserver/rule_flags.go)
        │
        ├─ rule.Flags.WebProxyService, else
        │  webproxy.ParseScenarioService(scenarioConfig.Extensions)
        │       ⇒ one merged flags.WebProxyService
        │
        ├─ applyWebProxyService → webproxy.WithService(ctx, ref)
        │       the tool loop reads this back at execution time
        │
        └─ RulePreVendorTransforms ⇒ webproxy.NewToolTransform(true)
                 strip native web tools, inject the two function tools

per request — execution half
  <downstream model emits tingly_box_mcp__webproxy__web_search>
        │
  mcpserver loop classifies it virtual (tool_classification.go)
        │
  ProtocolHandler.CallMCPToolWithHooks
        ├─ WebProxyService.Handles(name)? ──► Service.Execute
        └─ else ─────────────────────────────► MCP executor
                                    │
                          ServiceFromContext(ctx) ⇒ {provider, model}
                          poolWebClient.Search / .Fetch
                                    │
                          text ⇒ tool result ⇒ loop continues
```

## The two halves

### Request half — ToolTransform (transform.go)

Runs in the chain's **preVendor** slot, so it sees the upstream-bound shape
after protocol conversion. Two edits:

1. **Strip native web tools.** `web_search` / `web_fetch` are *server* tools:
   the provider executes them. A downstream that doesn't implement them either
   rejects the request or silently drops the capability.
2. **Inject two function tools** so the downstream model still has a way to ask
   for a search or a fetch.

| Target shape | strip | inject |
|---|---|---|
| `*openai.ChatCompletionNewParams` | by name (`web_search`, `websearch`, `web_search_preview`, `web_fetch`, `webfetch`) | ✓ |
| `*anthropic.MessageNewParams` | union members (`OfWebSearchTool*`, `OfWebFetchTool*`) | ✓ |
| `*anthropic.BetaMessageNewParams` | union members | ✓ |
| `*responses.ResponseNewParams` | `OfWebSearch` / `OfWebSearchPreview` / by name | ✗ |

The Responses row is the important asymmetry: that target never enters the
server-side tool loop, so nothing could answer an injected tool call — it would
leak to the client as an unanswerable request. Stripping still applies (the
downstream still can't run the native tool); injection is withheld.

Existing tools always win on a name collision — re-declaring a name would make
the request invalid.

### Execution half — Service.Execute (service.go)

Dispatches on the bare tool name and calls the borrowed service. Every failure
path returns a **non-empty error ToolResult**, not just an error: the
downstream model is mid-tool-loop and an empty result strands it.

```
  ┌──────────────────────────────────────┬──────────────────────────┐
  │ condition                            │ tool result              │
  ├──────────────────────────────────────┼──────────────────────────┤
  │ name not in the webproxy namespace   │ error: not a proxy tool  │
  │ no service in ctx                    │ error: none configured   │
  │ Client == nil                        │ error: not configured    │
  │ arguments missing / malformed        │ error: requires "query"  │
  │ borrowed call returned an error      │ error: <upstream error>  │
  │ borrowed call returned empty         │ text: "No results."      │
  │ success                              │ text: <answer>           │
  └──────────────────────────────────────┴──────────────────────────┘
```

The empty case is deliberately *not* an error: the borrowed model ran and found
nothing, and saying so plainly stops the downstream model from retrying the
same query.

## poolWebClient (web_proxy_client.go)

Dispatches by the borrowed provider's `APIStyle`, mirroring `poolVisionClient`:

- **`APIStyleAnthropic`** — attaches the native server tool
  (`web_search_20250305` / `web_fetch_20250910`) and streams one turn, folding
  the events back with the shared assembler. Streaming for the same reason the
  vision proxy streams: several Anthropic-compatible gateways only implement
  the streaming endpoint for server-tool turns.
- **`APIStyleOpenAI`** — one plain non-streaming chat completion. OpenAI Chat
  has no portable native web tool, so this path relies on the borrowed model
  having built-in web access (Perplexity Sonar, `*-search-preview`, grounded
  gateway models). A model without it answers from memory.
- anything else — error, surfaced to the downstream model as a tool error.

## Not MCP

The tools use the `tingly_box_mcp__<source>__<tool>` naming scheme with source
`webproxy`, but the web proxy is **not** an MCP source: no MCP server, no
source config, no runtime registration, no entry in the callable-tool list.
The naming is borrowed so the tools travel through the existing server-side
tool loop — the path that already knows how to execute a tool in-process and
continue the conversation without the client ever seeing the call.

That is also why `CallMCPToolWithHooks` routes web proxy tools **before** the
MCP executor: the MCP guard would reject them as uncallable.

## Testing

- `webproxytest/` — shared test doubles (`StubWebClient`, context helper) for
  this package and for `internal/protocolserver` tests.
- `internal/protocoltest/flags.go` carries the end-to-end `web_proxy_service`
  case: one request that asserts both halves — the native tool never reaches
  the downstream model, the proxy tools do, and the configured web service is
  actually called when the model uses one.

## Out of scope (today)

- Caching search / fetch results across requests.
- Executing injected tools on the Responses target (no tool loop there yet).
- Rewriting `server_tool_use` / `web_search_tool_result` blocks already present
  in a conversation's history (only reachable when a conversation previously
  ran against a natively web-capable provider and is then re-routed).
