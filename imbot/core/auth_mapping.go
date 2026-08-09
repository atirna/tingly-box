// auth_mapping.go defines how a platform's stored credentials (a flat
// auth-map, as kept in the settings store) are translated into an AuthConfig
// and which of those entries are required before a bot may start. The mapping
// itself lives on each platform's PlatformDescriptor (the Auth field), so
// adding a platform is a single table edit rather than a hunt for switch
// statements over platform names.
package core

// AuthMapping says which auth-map keys feed which AuthConfig fields, and
// which of them a bot cannot start without.
//
// This exists so that adding a platform is a table edit rather than a hunt for
// every switch statement over platform names. Lark is the cautionary tale: it
// was present in this table but missing from two hand-written switches in the
// bot manager, so Lark bots were rejected as having no valid credentials and,
// had they got past that, would have been handed a token-type auth config that
// the Feishu client rejects.
type AuthMapping struct {
	// Type is the AuthConfig type: token, oauth, qr, none.
	Type string
	// TokenKey and friends name the auth-map key feeding each AuthConfig field.
	// An empty name means the field is not used by this platform.
	TokenKey        string
	ClientIDKey     string
	ClientSecretKey string
	AccountIDKey    string
	AuthDirKey      string
	// OptionKeys are auth-map entries forwarded verbatim into Config.Options
	// rather than into AuthConfig.
	OptionKeys []string
	// RequiredKeys must all be present and non-empty before a bot may start.
	RequiredKeys []string
}

// defaultAuthMapping is used for platforms with no Auth entry on their
// PlatformDescriptor. Historically that meant "assume a bot token", which is
// the least surprising guess for a new IM platform.
var defaultAuthMapping = AuthMapping{Type: "token", TokenKey: "token", RequiredKeys: []string{"token"}}

// AuthMappingFor returns a platform's auth mapping, falling back to the
// token-based default for unknown platforms or platforms with no Auth entry.
func AuthMappingFor(platform Platform) AuthMapping {
	if d, ok := platformByID[platform]; ok && d.Auth != nil && d.Auth.Type != "" {
		return *d.Auth
	}
	return defaultAuthMapping
}

// AuthMappingForID is the string-id variant of AuthMappingFor, for callers
// that hold a platform id rather than a typed Platform.
func AuthMappingForID(platform string) AuthMapping {
	return AuthMappingFor(Platform(platform))
}

// BuildAuthConfig converts a bot's stored auth map into an AuthConfig.
func BuildAuthConfig(platform string, auth map[string]string) AuthConfig {
	m := AuthMappingForID(platform)
	pick := func(key string) string {
		if key == "" {
			return ""
		}
		return auth[key]
	}
	return AuthConfig{
		Type:         m.Type,
		Token:        pick(m.TokenKey),
		ClientID:     pick(m.ClientIDKey),
		ClientSecret: pick(m.ClientSecretKey),
		AccountID:    pick(m.AccountIDKey),
		AuthDir:      pick(m.AuthDirKey),
	}
}

// MissingAuthKeys lists the required auth keys a bot has not been given. An
// empty result means the bot has everything it needs to attempt a connection.
func MissingAuthKeys(platform string, auth map[string]string) []string {
	var missing []string
	for _, key := range AuthMappingForID(platform).RequiredKeys {
		if auth[key] == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

// AuthOptions returns the auth-map entries a platform expects in Config.Options
// rather than in AuthConfig (Weixin's user_id / base_url).
func AuthOptions(platform string, auth map[string]string) map[string]interface{} {
	keys := AuthMappingForID(platform).OptionKeys
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		if v, ok := auth[key]; ok && v != "" {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
