# Python SDK (`tingly`) — design

> Audience: tingly-box contributors touching the SDK seam (`sdk/python/`), the
> `/api/v1/sdk/session` endpoint, or the `experiment` scenario.

Diagrams: `.design/python-sdk.pencil.md`.

## Why

tb is a capable personal-intelligence gateway, but extending or experimenting
on top of it meant either editing the Go backend or hand-rolling HTTP calls
with the right base URL, token and scenario path. There was no fast seam for
"I have an idea, let me try it against my box in ten lines".

## The one idea

> **`tingly` is two independent halves: `tingly.Server` lets you write a
> provider in Python; `tingly.connect()` lets you consume tb from Python.
> Wiring them together happens in the tb UI, the same way you'd add Ollama.**

```
  your Python process                      tingly-box                     real upstreams
 ┌──────────────────────┐            ┌────────────────────────┐         ┌──────────────┐
 │ tingly.Server("rag") │◄── (1) ────┤ provider: rag           │         │ Anthropic    │
 │  @srv.chat           │  /v1/msgs  │  api_base=:8765         │  (3)    │ OpenAI       │
 │  def handle(req):    │            │  no_key, self-hosted    ├────────►│ Ollama …     │
 │     ...              │            │  ← same path as Ollama  │         └──────────────┘
 │     srv.tb.ask(...) ─┼── (2) ────►│ ordinary rule/pipeline  │
 └──────────────────────┘  /tingly/  └────────────────────────┘
                           experiment
  (1) and (2) are separate capabilities.
  (1) alone      = a plain custom provider, as independent as any other.
  (1) + (2)      = a provider that can orchestrate the whole box.
```

Neither half needs the other. A Server that never calls back is a perfectly
good provider; a `connect()` experiment that never serves anything is a
perfectly good script.

## tb-side: there is no new concept

**A thing that answers LLM protocol is a provider. That is the whole model.**
tb already had every piece:

- `AuthType=vmodel` (`ai/provider.go`) is a first-class persisted provider
  type with its own DB columns (`internal/data/db/provider_store.go`) and
  `VModelDetail{Models, LatencyProfile}`. The in-process synthetic models are
  already providers.
- Ollama / LM Studio / LocalAI / Jan / vLLM / SGLang are already
  `region: "self-hosted"` provider templates (`internal/data/providers.json`):
  localhost base URL, `no_key_required`, `supports_models_endpoint`.
- Connect AI already has a **Self-hosted** section and a **Custom endpoint**
  path (`frontend/src/components/ConnectProviderDialog.tsx`).

So a Python process speaking `/v1/messages` + `/v1/chat/completions` is
indistinguishable from Ollama, and needs **no backend mechanism at all**. The
only tb-side change for this feature is one data row:

```jsonc
// internal/data/providers.json
"tingly-python": {
  "region": "self-hosted", "type": "self-hosted",
  "base_url_openai":    "http://localhost:8765/v1",
  "base_url_anthropic": "http://localhost:8765",
  "no_key_required": true, "supports_models_endpoint": true
}
```

Self-hosted cards emit `kind: 'local'` from the picker, which the existing
form hook turns into `noKeyRequired: true` + the pre-filled base URL — so the
entry works with zero new frontend code. `supports_models_endpoint` makes tb's
model-list refresh call the server's `GET /v1/models`, so the model id is
discovered rather than typed.

### Two implementations of one concept

| | `AuthType=vmodel` (in-process) | `tingly.Server` (out-of-process) |
|---|---|---|
| code | compiled into tb, Go, `vmodel/` | your own process, Python |
| tb sees | a provider | a provider |
| added by | seeded builtin | Connect AI → Self-hosted |
| dispatch | short-circuits to the in-process handler | ordinary outbound HTTP |
| can call back into tb | no (same process) | **yes** (`srv.tb`) — the reason it exists |

### What we cut, and why it's worth remembering

Four iterations over-built this seam before it collapsed to the row above.
Each was a reasonable-looking step that added a concept tb did not need:

1. A persisted "plugin provider kind" with its own DB column and a distinct
   registration endpoint.
2. A full ephemeral service-discovery layer — in-memory registry, per-instance
   lease, heartbeat thread, TTL expiry, a `Config` hook consulted on every
   provider lookup — built to avoid leaving a stale DB row behind when a
   process stopped. tb already has liveness detection: the per-service circuit
   breaker (`internal/loadbalance/breaker.go`) covers every `(rule, service)`
   pair. This was distributed-systems machinery for a single-operator box.
3. An idempotent `POST /api/v2/plugins` upsert plus a `"plugin"` provider tag
   and `Provider.IsPlugin()` — smaller, but still a second way to create a
   provider, and a second word for one that already had a name.
4. Python-side scaffolding for the above: a `tingly.toml` manifest, a
   `register.py`, and `tingly plugin init|run` CLI subcommands.

All four are gone. The tell was visible before the code was: an earlier
revision of this document carried a section titled *"⚠️ Naming collision to
resolve"*, noting that the frontend already uses **"Plugins"** for an
unrelated concept (per-rule feature flags — see `.design/rule-flags.md`), and
that per `.design/ux-principles.md` §3 one word may only mean one thing. The
resolution turned out not to be a rename. When a new name collides with an
existing one *and* the thing it names already has a name, the second concept
is the bug. Deleting it removed ~1,100 lines and every open follow-up that
depended on it (sub-process supervisor, `/plugins/<name>/*` reverse-proxy
mount, lifecycle UI).

**Kept from that work**, because it fixes a real and general bug:
`ai.NoKeySentinelToken` (`ai/provider.go`). `Provider.GetAccessToken()`
returned `""` for a no-key provider, and the vendored `anthropic-sdk-go`
treats an empty API key as "go look for ambient credentials" — it runs its own
discovery (env vars, `anthropic auth login` profile, …) and errors loudly when
none exist, instead of sending an empty/absent header the way the OpenAI
client does. Any `NoKeyRequired=true` Anthropic-style provider hits this; a
local Python server is simply the one that surfaced it.

## Shape

```
sdk/python/
  tingly/
    client.py        # Client + connect()  ← consume tb
    discovery.py     # probe gateway + POST /sdk/session
    config.py        # (base_url, admin_token) resolution precedence
    scenarios.py     # scenario + transport constants
    transports/      # build openai.OpenAI / anthropic.Anthropic bound to tb
    helpers/         # usage + guardrails views
    server/          # ← be a provider
      core.py        #   Server class (@srv.chat, .tb, .use, .run, .connect_hint)
      http.py        #   stdlib HTTP server: /v1/messages + /v1/chat/completions, + SSE
      types.py       #   ChatRequest / Message (from_anthropic_body / from_openai_body)
    cli.py           # `tingly doctor`
    errors.py        # TinglyError hierarchy
```

## Consume: request flow

```
connect(scenario="experiment")
   │
   ├─ config.resolve()           args → env → ~/.tingly-box/sdk.json → config.json → localhost
   ├─ discovery.probe_version()  GET  /api/v1/info/version   (liveness)
   ├─ discovery.create_session() POST /api/v1/sdk/session     (admin token → model token)
   └─ Client(session, gateway_url, admin_token)
          .openai      → openai.OpenAI(base_url = scenario_root + "/v1")
          .anthropic   → anthropic.Anthropic(base_url = scenario_root)
          .ask()       → Anthropic first when the scenario supports both, else OpenAI
          .usage       → GET /api/v1/requests        (admin token)
          .guardrails  → GET /api/v1/guardrails/config (admin token)
```

The SDK never talks to providers directly — upstreams are reachable **only**
through the gateway. That is the point: the experiment inherits
routing/fallback/guard-rails/quota for free. Provisioning uses the **admin**
token and the `/api/v1/*` control plane; inference uses the **model** token and
the `/tingly/:scenario` data plane. The inference path is unchanged tb
internals; the SDK contributes the `experiment` scenario and one provisioning
endpoint, nothing in the hot path.

### Two-token model

- **Admin token** (tb's `UserToken`): authorizes `POST /sdk/session`. Resolved
  from `TINGLY_BOX_TOKEN` / `sdk.json` / `config.json:UserToken`.
- **Model token** (tb's `ModelToken`): returned *by* the session, and used as
  the bearer for the actual LLM calls.

In v0.1 the session returns the existing long-lived model token (same as
`tbclient.GetConnectionConfig` / `GetClaudeCodeEnv` already do). Short-lived
scoped tokens (`expires_at`) are the obvious follow-up — the response field is
already present and `omitempty`.

### Gateway seam: `POST /api/v1/sdk/session`

Handler: `internal/server/sdk_session.go` (`CreateSDKSession`), registered in
`server_webui_api.go` under the authenticated `apiV1` group.

Request `{ scenario, name }` → response
`{ base_url, token, scenario, transport, ready, services, expires_at? }`.

- `base_url` is the scenario root `http://host:port/tingly/<scenario>`. Bind
  host `0.0.0.0`/`::` is rewritten to `127.0.0.1` so it's client-usable.
- `transport` is `openai`|`anthropic`|`both`, collapsed from the scenario
  descriptor's `SupportedTransport`.
- `ready`/`services` report whether an active rule with ≥1 service is bound, so
  `tingly doctor` can name the next action instead of failing opaquely.
- Unknown / non-bindable scenario → 404 with `valid_scenarios` in the body.

No new routes were needed for the LLM calls themselves: `/tingly/:scenario` and
`/tingly/:scenario/v1` are already dynamic.

### The `experiment` scenario

Added to `internal/typ/type.go` (`ScenarioExperiment`) and the descriptor
registry (`scenario_registry.go`): OpenAI + Anthropic transports,
rule-bindable, path-usable, profile-capable. It exists so SDK traffic has its
own isolated rule instead of polluting `claude_code` / `openai` rules — and so
users can name parallel experiments via profiles (`experiment:p1`).

## Serve: `tingly.Server`

```python
from tingly import Server

srv = Server(name="my-rag")          # model id: my-rag

@srv.chat
def handle(req):                     # req: ChatRequest
    docs = retrieve(req.last_user_text())
    return srv.tb.ask(f"Using {docs}, answer: {req.last_user_text()}")

if __name__ == "__main__":
    srv.run()                        # http://127.0.0.1:8765
```

Design choices:

- **No framework dependency.** `http.server.ThreadingHTTPServer`, so a
  provider is one `pip install tingly` away. It always serves **both**
  `POST /v1/messages` and `POST /v1/chat/completions` (buffered *and* real
  SSE), plus `GET /v1/models` and `GET /health`.
- **No `api_style` knob.** Both routes are always live, so which one tb calls
  is entirely the provider's `api_style` on the tb side. There was briefly a
  server-side setting mirroring it; two knobs for one fact is exactly the
  "one knob controlling two things" smell of `ux-principles` §4/§6.
- **Handler contract is minimal and protocol-agnostic.** Return a `str`
  (buffered) or an iterator of `str` (streamed); the server shapes it into
  `message`/SSE `message_*` for the Anthropic route or
  `chat.completion`/`chat.completion.chunk` for the OpenAI route.
  `ChatRequest.from_anthropic_body` folds Anthropic's top-level `system` field
  into a leading `role="system"` message so `req.system_text()` /
  `req.last_user_text()` work identically on both routes.
- **Model id is the name, unprefixed.** The provider is already the namespace.
- **Path-only routing.** tb calls Anthropic upstreams as
  `POST /v1/messages?beta=true`; the server routes on the path and ignores the
  query.
- **`srv.tb` is a lazy Layer-1 client**, `srv.use(scenario)` targets another
  rule-set. This is the recursion in the graph above, and the only reason an
  out-of-process provider beats an in-process vmodel.
- **Optional token auth.** `Server(api_key=...)` enforces a bearer token so
  only tb (carrying the matching provider token) can call it — checked once,
  ahead of both routes.
- **`connect_hint()` prints literal, pasteable values** on startup (base URLs,
  key, model). `ux-principles` §11: hand over the artifact for the next
  action; §5: show the concrete value.

### No CLI beyond `doctor`

A `Server` is a Python process you start with `python my_server.py`, and it is
added to tb through the ordinary Connect AI flow. Neither step wants a
bespoke command. `tingly doctor` remains because diagnosis genuinely needs to
traverse the real path (`ux-principles` §7); it ends by printing the
Connect AI values for the serving half.

## UX-principles alignment

- **§3 one word, one meaning.** "Plugin" no longer names two things; the
  frontend's rule-flag "Plugins" owns the word.
- **§2 no mode picker.** `connect()` is identical in dev and hosted contexts;
  the environment decides discovery. `Server` has no transport mode to choose.
- **§6 smart defaults.** `scenario="experiment"`, `model="auto"`, port 8765
  matching the provider template.
- **§5 / §11 concrete values, handed over.** `connect_hint()` and
  `tingly doctor` print pasteable base URLs; `ready=false` and
  `GuardrailBlockedError(policy_id, reason)` name what to fix.
- **§7 diagnostics traverse the real path.** `tingly doctor` runs the actual
  discover → session → live round-trip; `e2e_run.sh` deliberately creates the
  provider via the ordinary `POST /api/v1/providers`, proving no second path
  exists.

## Testing

- Python: `sdk/python/tests/` — config precedence, discovery/session (respx
  mocked gateway), transport URL shaping, client routing, the dual-protocol
  server over real HTTP, and the example handlers with `use()` faked.
  Integration tests needing a live tb are marked `@needs_tb`, skipped by
  default.
- Go: `internal/server/sdk_session_test.go` freezes the response JSON field
  names (contract with the SDK) and the transport-label logic;
  `internal/typ/scenario_registry_test.go` pins the experiment descriptor;
  `internal/data/provider_template_test.go` validates every embedded template
  including `tingly-python`.
- End-to-end: `sdk/python/examples/e2e_run.sh` — real `tb` binary, no network,
  no keys. See its header for the assertions.

## Open follow-ups

1. Scoped short-lived session tokens (`expires_at` + refresh on 401).
2. Dedicated `GET /api/v1/sdk/usage?session=` so usage doesn't scan
   `/api/v1/requests`.
3. Async client (`AsyncClient`, `aask`) — transports already have async
   builders.
