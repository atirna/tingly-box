# Device Code as an alternative to the callback flow

Exploration: can Claude Code and Codex be signed in **without** a localhost
callback, the way Qwen and Kimi already are?

Short answer:

| Issuer | Device-style flow available? | Verdict |
|---|---|---|
| **Codex** (OpenAI / ChatGPT) | **Yes** — proprietary `deviceauth` flow, not RFC 8628 | **Implementable today**, needs a new driver |
| **Claude Code** (Anthropic) | Endpoint exists, but our client is refused | **Blocked upstream** — use the paste-code fallback |
| Gemini / Antigravity (Google) | No — Google refuses device flow for desktop clients | Not possible with these client IDs |
| Qwen | Yes (RFC 8628 + PKCE) | Already shipped |
| Kimi Code | Yes (RFC 8628) | Already shipped |

Everything below marked *(probed)* was verified by direct HTTP request against
the live endpoints while writing this document.

## Why this matters

`callback` requires a browser **on the same machine that runs tingly-box** and a
free localhost port (Anthropic prefers `54545`, Codex *requires* `1455`). That
holds for the desktop app and breaks for every remote deployment: Docker, a VPS,
SSH, the GUI-less server mode. Device code moves the browser to whatever device
the user actually has one on, and needs no inbound port at all. So the two are
not competitors — callback is the better local experience, device code is the
only remote one, and a provider that supports both should offer both.

## Codex — proprietary `deviceauth`, fully specified

Source: `codex-rs/login/src/device_code_auth.rs` and `server.rs` in `openai/codex`.
It is **not** RFC 8628 — different endpoints, different field names, different
pending signal, and an extra final step. Our `ai/oauth/devicecode.go` cannot
drive it as-is.

Base: `issuer = https://auth.openai.com`, `client_id = app_EMoamEEZ73f0CkXaXp7hrann`
— the same public client already in `registry.go`, so no new credential needed.

1. **Request a user code**
   `POST {issuer}/api/accounts/deviceauth/usercode`, JSON `{"client_id": "..."}`
   → `{"device_auth_id": "...", "user_code": "...", "interval": "5"}`
   Note `interval` is a **string** in the response, not a number.
   `404` here means device login is disabled for that account (see gating below).
2. **Show the user** `{issuer}/codex/device` plus `user_code`. Expires in 15 min.
3. **Poll** `POST {issuer}/api/accounts/deviceauth/token`, JSON
   `{"device_auth_id": "...", "user_code": "..."}`.
   **`403` or `404` means "still pending" — keep polling.** There is no
   `authorization_pending` error body. Any other non-2xx is fatal.
   On `200` → `{"authorization_code": "...", "code_verifier": "...", "code_challenge": "..."}`.
4. **Exchange** — the poll does *not* return tokens. Do a normal PKCE exchange:
   `POST {issuer}/oauth/token`, form-encoded,
   `grant_type=authorization_code&code={authorization_code}&redirect_uri={issuer}/deviceauth/callback&client_id=...&code_verifier={code_verifier}`
   → `{id_token, access_token, refresh_token}`.

Because step 4 is the ordinary Codex token exchange, everything downstream —
`CodexHook`, the `id_token` / ChatGPT `account_id` extraction, refresh — is
unchanged. Only steps 1–3 are new.

**Gating (important for UX):** device-code sign-in is off by default. A personal
account enables it under ChatGPT → Settings → Security → device code
authorization; a workspace member needs an admin to allow it in workspace
permissions. So a `404` at step 1 is a *configuration* answer, not a failure —
it must be surfaced as "enable device code authorization in your ChatGPT
settings, or use the callback flow instead", with the callback flow one click away.

## Claude Code — the endpoint is real, our client is not allowed

*(probed)* `POST https://api.anthropic.com/v1/oauth/device_authorization` is a
live endpoint. It takes JSON and validates fields (`client_id` required,
`scope` required). Two responses distinguish the situation precisely:

- a made-up client ID → `{"error": "invalid_client"}`
- the Claude Code client ID `9d1c250a-…` → `{"error": "unauthorized_client"}`

and the token endpoint is explicit:

```
POST https://api.anthropic.com/v1/oauth/token
{"grant_type": "urn:ietf:params:oauth:grant-type:device_code", "client_id": "9d1c250a-…"}
→ 400 {"error": "unauthorized_client",
       "error_description": "Client is not authorized for the device_code grant type."}
```

So Anthropic has implemented RFC 8628 server-side and reserved it for some other
client; the public Claude Code client is deliberately excluded. This is not
something we can work around, and we should not try — the fix is upstream
(`anthropics/claude-code#22992` tracks the request). Worth re-probing
periodically: if that client is ever whitelisted, Claude Code drops straight
into the existing `OAuthMethodDeviceCode` path with only a registry entry.

**Fallback that gets most of the benefit today.** The Claude Code authorize
endpoint accepts a redirect to `https://console.anthropic.com/oauth/code/callback`,
which renders the `code#state` pair for the user to copy back instead of
redirecting to localhost. That is a *manual paste* flow, not device code — the
user needs clipboard access between the two machines and there is no polling —
but it removes the localhost-listener requirement, which is the actual blocker
for remote deployments. This one is **not probed** (claude.ai sits behind
Cloudflare and refuses non-browser requests); it is the mechanism the CLI
ecosystem uses and should be confirmed against a live sign-in before we build on it.

## Gemini / Antigravity — no

*(probed)* `POST https://oauth2.googleapis.com/device/code` with either the
Gemini CLI or the Antigravity client ID returns
`401 {"error": "invalid_client", "error_description": "Invalid client type."}`.
Google only permits the device grant for OAuth clients registered as "TV and
Limited Input device", and both of ours are desktop clients. Registering a
limited-input client of our own would also not help: Google restricts that
client type to a small scope allow-list that excludes `cloud-platform`.

## What the code would need

Today `ProviderConfig.OAuthMethod` is a single enum that conflates two
independent things: **which grant** (auth code vs device) and **whether PKCE**.
That is why `OAuthMethodDeviceCodePKCE` has to exist as a separate value, and it
is also why a provider cannot currently offer *both* callback and device — the
enum picks one, and `handler.go:392` branches on it. Codex is the first issuer
where both are real, so this is the point where the axes should separate
(`.design/ux-principles.md`, "separate orthogonal axes"). Two changes:

1. **Make flow a per-request choice, not a provider property.** Give
   `ProviderConfig` a set of supported flows and let `AuthorizeOAuth` take an
   optional flow, defaulting by deployment context rather than by asking:
   desktop/local → callback, headless/remote → device code. No mode picker in
   the common case ("smart defaults over toggles"), with the other flow offered
   as an escape hatch — and offered *automatically* when the default fails
   (port 1455 busy, or the ChatGPT `404` above).
2. **Make the device flow pluggable.** `InitiateDeviceCodeFlow` /
   `PollForToken` currently hardcode RFC 8628 wire shapes. Codex needs different
   endpoints, string-typed `interval`, 403/404-as-pending, and a trailing PKCE
   exchange. Rather than special-casing Codex inside `devicecode.go`, add a
   `DeviceFlowDriver` alongside `RequestHook`:

   ```go
   type DeviceFlowDriver interface {
       RequestDeviceCode(ctx context.Context, cfg *ProviderConfig, opts *Options) (*DeviceCodeData, error)
       PollDeviceToken(ctx context.Context, cfg *ProviderConfig, data *DeviceCodeData, opts *Options) (*Token, error)
   }
   ```

   with the current RFC 8628 implementation as the default driver (Qwen, Kimi,
   and Anthropic-if-it-ever-opens keep using it untouched) and a `CodexDeviceDriver`
   for the proprietary variant. `DeviceCodeData` already carries everything Codex
   needs except `device_auth_id`, which fits in a small opaque `Extra` field.

The API surface is already right: `OAuthAuthorizeResponse` returns
`user_code` / `verification_uri` / `interval` and `pollForDeviceCodeToken` runs
polling in the background against the session state
(`internal/server/module/oauth/handler.go`). A Codex device login reuses all of
it, and the frontend gets no new response shape.

## Recommendation

- Build the `DeviceFlowDriver` seam and land **Codex device code** — it is the
  only issuer where this unblocks real headless usage today, and the wire
  protocol above is complete enough to implement directly.
- For **Claude Code**, do not build anything against `device_authorization`; it
  will fail for every user. If remote Claude sign-in is needed sooner, spike the
  paste-code redirect instead, after confirming it against a live sign-in.
- Keep the callback flow as the default wherever a local browser exists. Device
  code is the fallback that makes remote work, not a replacement.
