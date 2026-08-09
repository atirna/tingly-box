# imbot/platform

Each subdirectory implements `core.Bot` for one messaging platform. All platforms share the same lifecycle, event model, and message types from `imbot/core`; differences are encapsulated in per-package **Adapter** (inbound events) and **action renderers** (outbound `core.ActionSet`).

## Platforms

| Platform | README | Auth | Connection |
|---|---|---|---|
| Telegram | [telegram/](telegram/README.md) | token | Long-polling |
| Discord | [discord/](discord/README.md) | token | WebSocket (Gateway) |
| Slack | [slack/](slack/README.md) | token | RTM |
| Feishu | [feishu/](feishu/README.md) | oauth | WebSocket (Event Push) |
| Lark | [lark/](lark/README.md) | oauth | WebSocket (Event Push) |
| DingTalk | [dingtalk/](dingtalk/README.md) | oauth | Stream SDK |
| Weixin | [weixin/](weixin/README.md) | token | WebSocket |
| WeCom | [wecom/](wecom/README.md) | oauth | WebSocket |
| WhatsApp | [whatsapp/](whatsapp/README.md) | token | REST (Meta Cloud API) |
| Tingly | [tingly/](tingly/README.md) | none | InProcess / pluggable |

## Registry

Platform constructors are mapped from `core.Platform` values to factory
functions in `imbot/registry.go` (`RegisterBuiltinPlatforms`). `imbot/platform/`
is a directory of packages, not a package itself — the global helpers live on
the top-level `imbot` package:

```go
bot, err := imbot.Create(config)
ok := imbot.IsSupported(core.PlatformSlack)
```

Register a custom platform at startup:

```go
imbot.Register(core.Platform("myplatform"), func(cfg *core.Config) (core.Bot, error) {
    return mypkg.NewBot(cfg), nil
})
```

## Adding a new platform

1. Create `platform/<name>/<name>.go` embedding `*core.BaseBot`.
2. Implement `core.Bot` (connect, disconnect, send, react, edit, delete).
3. Add an `Adapter` that converts raw SDK events to `core.Message` and calls `b.EmitMessage(msg)`.
4. Add the platform to the `platformDescriptors` table in `core/platforms.go` — display name, capabilities, behavior defaults, and auth metadata (`AuthType`, `Category`, and `Auth *AuthMapping` describing how a stored auth-map becomes a `core.AuthConfig`). This is the single registration site for all intrinsic platform attributes.
5. Register the constructor in `imbot/registry.go` → `RegisterBuiltinPlatforms()`.
6. If the platform has a settings form, add its UI fields (labels, placeholders, secret flags) to the `platformFormFields` map in `imbot/auth.go`. The intrinsic fields (`AuthType`/`Category`/`DisplayName`) are already derived from core, so do not duplicate them here.
7. Optionally add an `action_render.go` that renders `core.ActionSet` natively, and implement `core.MessageRestater` (see telegram/feishu for the pattern).
8. Add a `README.md` following the pattern of existing platforms.
