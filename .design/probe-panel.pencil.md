# Probe Panel Redesign (pencil)

Wireframes for `.sdlc/docs/probe-panel-redesign-20260820.spec.md`.

Legend: `▤` = toggle group · `( )` = disabled w/ tooltip · `▸` = collapsed section ·
`$VAR` = placeholder secret · `→` = derived/resolved value.

## 1. Axis model — before vs after

```
  BEFORE (one mixed enum + hidden knobs)              AFTER (orthogonal axes)

  test_mode ──┬─ simple     (nonstream)               Shape     ▤ Nonstream | Stream
              ├─ streaming  (stream)                  Tool      ▤ Off | On
              └─ tool       (stream + tools)    ⇒     Thinking  ▤ none | low | medium | high
  endpoint: exists in API, NO UI                      Protocol  ▤ OA-Chat | OA-Resp | Anthropic
  direct:  ▤ Through TB | Direct                      Scope     ▤ Through TB | Direct
                (openai-only, 2 values)               Message   [ editable, default per Shape/Tool ]

One knob per axis; every future knob is one more row, not a new mode.
Protocol has NO "Auto" — always a concrete wire protocol, reduced per target (see §2).
Labels are brand-first (matches ApiStyleBadge vocabulary): full labels in the UI are
"OpenAI Chat" / "OpenAI Responses" / "Anthropic" — bare "Responses"/"Messages" would
assume SDK knowledge; "Anthropic" needs no suffix (single protocol in that family).
```

## 2. Dialog layout (provider target — richest case)

```
┌─ Probe · Kimi · kimi-k2-0905-preview ──────────────── [⧉ cURL] [⟳] [▶ Run] ─┐
│                                                                                │
│  REQUEST CONFIG                                                                │
│  ┌──────────────────────────────────────────────────────────────────────────┐  │
│  │  Shape     ▤ Nonstream │ Stream        Tool       ▤ Off │ On             │  │
│  │  Thinking  ▤ none │ low │ med │ high   Protocol  ▤ OpenAI Chat │ OA-Resp │ Anthropic │
│  │  Scope     ▤ Through TB │ Direct        Message   [Hello, this is a te…] │  │
│  └──────────────────────────────────────────────────────────────────────────┘  │
│                                                                                │
│  ▸ cURL  ── mirrors the config above · copy ⧉ ──────────────────────────────   │
│  │   (expanded:)                                                               │
│  │   curl -N http://localhost:9999/tingly/openai/v1/chat/completions \        │
│  │     -H "Authorization: Bearer $TB_API_KEY" \                                │
│  │     -H "Content-Type: application/json" \                                   │
│  │     -d '{"model":"kimi-k2-…","stream":true,"messages":[…]}'                 │
│  │   caption: replace $TB_API_KEY with your gateway key                        │
│                                                                                │
│  ✅ Success · 850ms · 43 tokens                                                 │
│  ▸ Request Journey   Rule → Flags → Routing → Provider → Endpoint → URL        │
│  ▸ Response          (extracted text)                                           │
│  ▸ Raw JSON          (raw upstream payload)                                    │
└────────────────────────────────────────────────────────────────────────────────┘
```

Target-type degradation (Protocol reduces to the concrete protocols the target
speaks — unavailable options simply don't render; single-protocol ⇒ locked):

```
  openai-style target:    Protocol ▤ OpenAI Chat │ OpenAI Resp.    (2 options)
  codex OAuth provider:   Protocol ▤ OpenAI Resp. │ OpenAI Chat     (default Responses)
  anthropic-style target: Protocol ( Anthropic )  ← locked to the only option
  dual-base provider:     Protocol ▤ OpenAI Chat │ OA-Resp │ Anthropic (3 options)
  google-style target:    Protocol ( – )  ← disabled; own SDK, out of scope
                          Scope   ( Direct )  ← locked; no loopback route
  rule target:            Scope  ( Through TB )  ← locked; tooltip: "rule probes must
                                                          traverse TB middleware"
  Tool axis needs no lock: both Tool×Shape combos are valid (non-stream lifts
  structured tool_calls; stream keeps raw chunks) — verified in helper.go.
```

## 3. Open-time state resolution (the stream-association fix)

```
                 dialog opens
                      │
                      ▼
        ┌───────────────────────────────┐
        │ explicit props?               │── yes ──► use them (entries stop hardcoding)
        └───────────────────────────────┘
                      │ no
                      ▼
        ┌───────────────────────────────┐
        │ initialResult present?        │── yes ──► Shape := result.stream
        └───────────────────────────────┘            (show that result, state matches it)
                      │ no
                      ▼
        ┌───────────────────────────────┐
        │ last-used config for this     │── yes ──► restore (localStorage,
        │ target-type?                  │            key: tb.probe.config.{rule|provider})
        └───────────────────────────────┘
                      │ no / first ever
                      ▼
              default: Stream · Tool off · Thinking none · Protocol = target's
                       primary (OpenAI→Chat · Codex→Responses · Anthropic→Messages)
                       · Through TB

  any manual change or re-run ──► persists back to tb.probe.config.{target-type}
```

## 4. cURL generation path (backend, shares the real request builder)

```
  dialog config change ──debounce──► POST /api/v2/probe/curl   (does NOT execute)
                                          │
                                          ▼
                          ┌───────────────────────────────────────┐
                          │ ValidateE2ERequest  (same as /probe)  │
                          │ BuildProbeRequest(req)  ← SAME builder │
                          │   the prober uses (single source of    │
                          │   truth — no parallel impl)            │
                          └───────────────────────────────────────┘
                                          │
                          ┌───────────────┴────────────────┐
                          ▼                                ▼
                  Scope = Through TB                Scope = Direct
                  URL: localhost:port               URL: provider.APIBase + path
                      /tingly/{scenario}/v1/…           (real upstream)
                  Auth: $TB_API_KEY                   Auth: $UPSTREAM_API_KEY
                  (header per scenario protocol)      (header per provider protocol)
                          └───────────────┬────────────────┘
                                          ▼
                          { command, request: {method, url, headers, body} }
                                          │
                                          ▼
                          dialog cURL section renders + copy ⧉
```
