package imbot

import (
	"fmt"

	"github.com/tingly-dev/tingly-box/imbot/core"
	"github.com/tingly-dev/tingly-box/imbot/platform/dingtalk"
	"github.com/tingly-dev/tingly-box/imbot/platform/discord"
	"github.com/tingly-dev/tingly-box/imbot/platform/feishu"
	"github.com/tingly-dev/tingly-box/imbot/platform/lark"
	"github.com/tingly-dev/tingly-box/imbot/platform/slack"
	"github.com/tingly-dev/tingly-box/imbot/platform/telegram"
	"github.com/tingly-dev/tingly-box/imbot/platform/tingly"
	"github.com/tingly-dev/tingly-box/imbot/platform/wecom"
	"github.com/tingly-dev/tingly-box/imbot/platform/weixin"
	"github.com/tingly-dev/tingly-box/imbot/platform/whatsapp"
)

// Registry manages platform bot implementations
type Registry struct {
	creators map[core.Platform]BotCreator
}

// BotCreator creates a bot instance
type BotCreator func(*core.Config) (core.Bot, error)

// NewRegistry creates a new platform registry
func NewRegistry() *Registry {
	r := &Registry{
		creators: make(map[core.Platform]BotCreator),
	}

	// Register built-in platforms
	r.RegisterBuiltinPlatforms()

	return r
}

// Register registers a platform bot creator
func (r *Registry) Register(platform core.Platform, creator BotCreator) {
	r.creators[platform] = creator
}

// Create creates a bot instance for the given platform
func (r *Registry) Create(config *core.Config) (core.Bot, error) {
	creator, ok := r.creators[config.Platform]
	if !ok {
		return nil, fmt.Errorf("unsupported platform: %s", config.Platform)
	}

	return creator(config)
}

// IsSupported checks if a platform is supported
func (r *Registry) IsSupported(platform core.Platform) bool {
	_, ok := r.creators[platform]
	return ok
}

// Platforms returns all supported platforms
func (r *Registry) Platforms() []core.Platform {
	platforms := make([]core.Platform, 0, len(r.creators))
	for platform := range r.creators {
		platforms = append(platforms, platform)
	}
	return platforms
}

// RegisterBuiltinPlatforms registers all built-in platforms
func (r *Registry) RegisterBuiltinPlatforms() {
	// Telegram
	r.Register(core.PlatformTelegram, func(config *core.Config) (core.Bot, error) {
		return telegram.NewTelegramBot(config)
	})

	// Discord
	r.Register(core.PlatformDiscord, func(config *core.Config) (core.Bot, error) {
		return discord.NewDiscordBot(config)
	})

	// Slack
	r.Register(core.PlatformSlack, func(config *core.Config) (core.Bot, error) {
		return slack.NewSlackBot(config)
	})

	// Feishu
	r.Register(core.PlatformFeishu, func(config *core.Config) (core.Bot, error) {
		return feishu.NewFeishuBot(config)
	})

	// Lark (alias to Feishu with different domain)
	r.Register(core.PlatformLark, func(config *core.Config) (core.Bot, error) {
		return lark.NewBot(config)
	})

	// WhatsApp
	r.Register(core.PlatformWhatsApp, func(config *core.Config) (core.Bot, error) {
		return whatsapp.NewWhatsAppBot(config)
	})

	// Tingly: full-featured platform that doubles as the E2E test harness.
	r.Register(core.PlatformTingly, tingly.NewBotFromConfig)

	// DingTalk
	r.Register(core.PlatformDingTalk, func(config *core.Config) (core.Bot, error) {
		return dingtalk.NewDingTalkBot(config)
	})

	// Weixin
	r.Register(core.PlatformWeixin, func(config *core.Config) (core.Bot, error) {
		return weixin.NewBot(config)
	})

	// WeCom (Enterprise WeChat AI Bot)
	r.Register(core.PlatformWecom, func(config *core.Config) (core.Bot, error) {
		return wecom.NewBot(config)
	})

	// Add more platforms as they are implemented
	// Google Chat, Signal, BlueBubbles
}

// Global registry instance
var globalRegistry = NewRegistry()

// Register registers a platform in the global registry
func Register(platform core.Platform, creator BotCreator) {
	globalRegistry.Register(platform, creator)
}

// Create creates a bot using the global registry
func Create(config *core.Config) (core.Bot, error) {
	return globalRegistry.Create(config)
}

// CreateBot creates a bot instance based on the configuration
func CreateBot(config *core.Config) (core.Bot, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Expand environment variables
	config.ExpandEnvVars()

	// Create bot using platform registry
	return Create(config)
}

// Platforms returns a list of all supported platforms
func Platforms() []string {
	ps := globalRegistry.Platforms()
	platforms := make([]string, len(ps))
	for i, p := range ps {
		platforms[i] = string(p)
	}
	return platforms
}
