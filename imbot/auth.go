package imbot

import "github.com/tingly-dev/tingly-box/imbot/core"

// FieldSpec defines a single auth field for the settings UI.
type FieldSpec struct {
	Key         string `json:"key"`         // Field key in auth map
	Label       string `json:"label"`       // Display label for the field
	Placeholder string `json:"placeholder"` // Placeholder text
	Required    bool   `json:"required"`    // Whether this field is required
	Secret      bool   `json:"secret"`      // Whether this field should be masked (password/token)
	HelperText  string `json:"helperText"`  // Additional guidance for users
}

// PlatformAuthConfig is the settings-API view of a platform: the intrinsic
// attributes (platform id, display name, auth type, category — all sourced from
// core.PlatformDescriptor, the single source of truth) together with the
// settings-UI form fields. It is what GetPlatforms / GetPlatformConfig return
// to the frontend.
type PlatformAuthConfig struct {
	Platform    string            `json:"platform"`     // Platform identifier
	AuthType    string            `json:"auth_type"`    // "token", "oauth", "qr", "none"
	DisplayName string            `json:"display_name"` // Human-readable platform name
	Category    string            `json:"category"`     // "im", "enterprise", "business"
	Fields      []FieldSpec       `json:"fields"`       // Settings-UI form fields
	Auth        core.AuthMapping  `json:"-"`            // Wire mapping (auth-map -> core.AuthConfig), from core
}

// platformFormFields holds the per-platform settings-UI form (labels,
// placeholders, secret flags, helper text). This is the only auth metadata
// that is genuinely imbot/UI-local: the intrinsic attributes (auth type,
// category, display name, wire mapping) live in core.PlatformDescriptor, and
// this map is keyed by the same platform ids so the two cannot drift.
//
// Platforms with no form (credentials arrive via a non-form flow, like Weixin's
// QR onboarding, or none at all like Tingly) have no entry here.
var platformFormFields = map[string][]FieldSpec{
	"telegram": {
		{
			Key:         "token",
			Label:       "Bot Token",
			Placeholder: "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
			Required:    true,
			Secret:      true,
			HelperText:  "Get from @BotFather on Telegram",
		},
	},
	"slack": {
		{
			Key:         "token",
			Label:       "Bot Token",
			Placeholder: "xoxb-your-token-here",
			Required:    true,
			Secret:      true,
			HelperText:  "Must start with 'xoxb-'. Get from Slack API",
		},
	},
	"discord": {
		{
			Key:         "token",
			Label:       "Bot Token",
			Placeholder: "MTIzNDU2Nzg5OABCDEF123456789",
			Required:    true,
			Secret:      true,
			HelperText:  "Must start with 'Bot ' prefix. Get from Discord Developer Portal",
		},
	},
	"dingtalk": {
		{
			Key:         "clientId",
			Label:       "App Key",
			Placeholder: "ding-your-app-key",
			Required:    true,
			Secret:      true,
			HelperText:  "Also known as AppKey or ClientId",
		},
		{
			Key:         "clientSecret",
			Label:       "App Secret",
			Placeholder: "Your app secret",
			Required:    true,
			Secret:      true,
			HelperText:  "Also known as AppSecret or ClientSecret",
		},
	},
	"feishu": {
		{
			Key:         "clientId",
			Label:       "App ID",
			Placeholder: "cli-your-app-id",
			Required:    true,
			Secret:      true,
			HelperText:  "Also known as AppID or ClientId",
		},
		{
			Key:         "clientSecret",
			Label:       "App Secret",
			Placeholder: "Your app secret",
			Required:    true,
			Secret:      true,
			HelperText:  "Also known as AppSecret or ClientSecret",
		},
	},
	"lark": {
		{
			Key:         "clientId",
			Label:       "App ID",
			Placeholder: "cli-your-app-id",
			Required:    true,
			Secret:      true,
			HelperText:  "Also known as AppID or ClientId",
		},
		{
			Key:         "clientSecret",
			Label:       "App Secret",
			Placeholder: "Your app secret",
			Required:    true,
			Secret:      true,
			HelperText:  "Also known as AppSecret or ClientSecret",
		},
	},
	"whatsapp": {
		{
			Key:         "token",
			Label:       "Access Token",
			Placeholder: "Your WhatsApp access token",
			Required:    true,
			Secret:      true,
			HelperText:  "Get from Meta for Developers",
		},
		{
			Key:         "phoneNumberId",
			Label:       "Phone Number ID",
			Placeholder: "Your phone number ID",
			Required:    false,
			Secret:      false,
			HelperText:  "Optional: The phone number ID for sending messages",
		},
	},
	"wecom": {
		{
			Key:         "clientId",
			Label:       "Bot ID",
			Placeholder: "Your WeCom AI Bot ID",
			Required:    true,
			Secret:      false,
			HelperText:  "The AI Bot ID from WeCom developer console",
		},
		{
			Key:         "clientSecret",
			Label:       "Bot Secret",
			Placeholder: "Your WeCom AI Bot secret",
			Required:    true,
			Secret:      true,
			HelperText:  "The AI Bot secret from WeCom developer console",
		},
	},
}

// CategoryLabels provides display labels for categories.
var CategoryLabels = map[string]string{
	"im":         "IM Platforms",
	"enterprise": "Enterprise",
	"business":   "Business",
}

// GetPlatformConfig returns the settings-API view for a given platform.
// Intrinsic fields come from core.PlatformDescriptor; the form fields come from
// this package's platformFormFields map.
func GetPlatformConfig(platform string) (PlatformAuthConfig, bool) {
	if !core.IsValidPlatform(platform) {
		return PlatformAuthConfig{}, false
	}
	return platformAuthConfig(platform), true
}

// GetAllPlatforms returns the settings-API view for every known platform.
// The platform set is sourced from core.PlatformNames (derived from the single
// core.PlatformDescriptor table).
func GetAllPlatforms() []PlatformAuthConfig {
	out := make([]PlatformAuthConfig, 0, len(core.PlatformNames))
	for id := range core.PlatformNames {
		out = append(out, platformAuthConfig(string(id)))
	}
	return out
}

// platformAuthConfig assembles a PlatformAuthConfig for one platform id from
// the core descriptor (intrinsic fields) and the local form-fields map.
func platformAuthConfig(id string) PlatformAuthConfig {
	p := core.Platform(id)
	return PlatformAuthConfig{
		Platform:    id,
		AuthType:    core.GetPlatformAuthType(p),
		DisplayName: core.GetPlatformName(p),
		Category:    core.GetPlatformCategory(p),
		Fields:      platformFormFields[id],
		Auth:        core.AuthMappingFor(p),
	}
}
