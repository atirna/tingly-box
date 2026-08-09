# imbot/core

Platform-neutral vocabulary and base machinery shared by every platform
implementation. Nothing in this package knows about a concrete platform SDK.
See `doc.go` for the package overview; the file naming exposes three internal
clusters: the command system (`command_*.go`), the pairing/security primitives
(`pairing*.go`), and the interaction vocabulary (`action.go`, `card.go`,
`keyboard.go`, `payload.go`).

## Contents

### Bot contract & base machinery

| File | Responsibility |
|---|---|
| `doc.go` | Package documentation |
| `bot.go` | The `Bot` interface, `SendMessageOptions`, `SendResult` |
| `base.go` | `BaseBot` — status, event handlers, chunking; embedded by all platform bots |
| `types.go` | Platform ids, chat types, parse modes, segments, capabilities |
| `platforms.go` | `PlatformDescriptor` table — the single source of truth for per-platform display names, capabilities, reactions, and behavior defaults |
| `config.go` | Bot configuration and validation |
| `errors.go` | `BotError` and typed error codes |
| `logger.go` | Injectable per-bot `Logger` interface |

### Messages

| File | Responsibility |
|---|---|
| `message.go` | Inbound `Message` and its `Content` variants |
| `message_builder.go` | Fluent builder over `Message` |
| `segments.go` | Multi-segment (thinking/body) outbound text helpers |
| `media_url.go` | Media URL normalization |
| `adapter.go` | `EventAdapter` — converts platform-specific events to unified `core.Message` |
| `handler.go` | Generic content-handler registry used by platform adapters (unrelated to commands) |

### Interaction vocabulary (outbound)

| File | Responsibility |
|---|---|
| `action.go` | `Action` / `ActionSet` — the neutral outbound interactive payload |
| `card.go` | `Card` builder and card-action types |
| `keyboard.go` | Inline keyboard buttons and the keyboard builder |
| `payload.go` | `Payload` — button identity as ordered segments |
| `restate.go` | `MessageRestater` — "this message is stale, replace it" capability |

### Command system

| File | Responsibility |
|---|---|
| `command_registry.go` | `CommandRegistry` — named commands with aliases, handlers, and platform-native menu rendering |
| `command_builder.go` | Fluent builder for defining commands |
| `command_context.go` | `HandlerContext` — execution context handed to a command handler |

### Pairing / security

| File | Responsibility |
|---|---|
| `pairing.go` | `PairingManager` — trust-on-first-use pairing: code minting, constant-time verify, TTL, lockout; pairing events are logged directly through the package logger |

Design rationale for actions, payloads, capabilities, and restate lives in
`.design/imbot-platform-seams.md` at the repo root.
