package core

import (
	"fmt"
	"os"
	"strings"
)

// Config represents the bot configuration
type Config struct {
	UUID     string                 `json:"uuid" yaml:"uuid"`
	Platform Platform               `json:"platform" yaml:"platform"`
	Enabled  bool                   `json:"enabled" yaml:"enabled"`
	Auth     AuthConfig             `json:"auth" yaml:"auth"`
	Options  map[string]interface{} `json:"options,omitempty" yaml:"options,omitempty"`
	Logging  *LoggingConfig         `json:"logging,omitempty" yaml:"logging,omitempty"`
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Type string `json:"type" yaml:"type"` // "token", "qr", "oauth", "basic", "serviceAccount"

	// Token auth
	Token string `json:"token,omitempty" yaml:"token,omitempty"`

	// Basic auth
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`

	// OAuth
	ClientID     string `json:"clientId,omitempty" yaml:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty"`
	RedirectURI  string `json:"redirectUri,omitempty" yaml:"redirectUri,omitempty"`

	// Service Account
	ServiceAccountJSON string `json:"serviceAccountJson,omitempty" yaml:"serviceAccountJson,omitempty"`

	// QR Auth options
	AuthDir   string `json:"authDir,omitempty" yaml:"authDir,omitempty"`
	AccountID string `json:"accountId,omitempty" yaml:"accountId,omitempty"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level      string `json:"level" yaml:"level"` // "debug", "info", "warn", "error", "silent"
	Timestamps bool   `json:"timestamps" yaml:"timestamps"`
}

// ManagerConfig represents the bot manager configuration
type ManagerConfig struct {
	AutoReconnect        bool `json:"autoReconnect" yaml:"autoReconnect"`
	MaxReconnectAttempts int  `json:"maxReconnectAttempts" yaml:"maxReconnectAttempts"`
	ReconnectDelayMs     int  `json:"reconnectDelayMs" yaml:"reconnectDelayMs"`
}

// DefaultManagerConfig returns default manager configuration
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		AutoReconnect:        true,
		MaxReconnectAttempts: 5,
		ReconnectDelayMs:     5000,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if !IsValidPlatform(string(c.Platform)) {
		return fmt.Errorf("invalid platform: %s", c.Platform)
	}

	// Validate auth config
	if err := c.Auth.Validate(); err != nil {
		return fmt.Errorf("invalid auth config: %w", err)
	}

	return nil
}

// Validate validates the auth configuration
func (a *AuthConfig) Validate() error {
	switch a.Type {
	case "token":
		if a.Token == "" {
			return fmt.Errorf("token is required for token auth")
		}
	case "basic":
		if a.Username == "" {
			return fmt.Errorf("username is required for basic auth")
		}
	case "oauth":
		if a.ClientID == "" || a.ClientSecret == "" {
			return fmt.Errorf("clientId and clientSecret are required for oauth")
		}
	case "serviceAccount":
		if a.ServiceAccountJSON == "" {
			return fmt.Errorf("serviceAccountJson is required for service account auth")
		}
	case "qr":
		// QR auth has no required fields
	case "none":
		// Tokenless auth (e.g. tingly): no fields required.
	default:
		return fmt.Errorf("unknown auth type: %s", a.Type)
	}

	return nil
}

// expandEnvVar returns the value of the environment variable named by s (s must
// start with "$"), or "" when the variable is unset. It is the single rule for
// "$VAR" expansion across every auth field — previously Token/Password went
// through GetToken/GetPassword while the OAuth/SA fields called os.Getenv
// directly, which made an unset variable behave differently per field.
func expandEnvVar(s string) string {
	return os.Getenv(strings.TrimPrefix(s, "$"))
}

// expandField expands s when it carries the "$VAR" prefix, leaving literal
// values untouched. Returns the (possibly expanded) string and whether s was a
// reference — so callers like GetToken can distinguish "unset env var" from
// "no token configured" and surface a meaningful error.
func expandField(s string) (value string, wasRef bool) {
	if strings.HasPrefix(s, "$") {
		return os.Getenv(strings.TrimPrefix(s, "$")), true
	}
	return s, false
}

// GetToken returns the token, resolving a "$VAR" reference to its environment
// variable. It errors only when the reference names an unset variable — a
// literal empty token is not an error here (validation of "required" happens in
// Validate). CreateBot runs ExpandEnvVars before any bot starts, so by the time
// a bot calls this the reference is usually already resolved; the method keeps
// the resolution so direct construction without ExpandEnvVars still works.
func (a *AuthConfig) GetToken() (string, error) {
	v, wasRef := expandField(a.Token)
	if wasRef && v == "" {
		return "", fmt.Errorf("environment variable %s is not set", strings.TrimPrefix(a.Token, "$"))
	}
	return v, nil
}

// GetPassword returns the password, resolving a "$VAR" reference. See GetToken.
func (a *AuthConfig) GetPassword() (string, error) {
	v, wasRef := expandField(a.Password)
	if wasRef && v == "" {
		return "", fmt.Errorf("environment variable %s is not set", strings.TrimPrefix(a.Password, "$"))
	}
	return v, nil
}

// ExpandEnvVars resolves "$VAR" references in every auth field in place, using
// the single expandEnvVar rule. An unset variable leaves the field holding the
// empty string consistently across Token, Password, ClientID, ClientSecret and
// ServiceAccountJSON — callers detect a missing credential via Validate, not by
// catching an error from one specific field.
func (c *Config) ExpandEnvVars() {
	if strings.HasPrefix(c.Auth.Token, "$") {
		c.Auth.Token = expandEnvVar(c.Auth.Token)
	}
	if strings.HasPrefix(c.Auth.Password, "$") {
		c.Auth.Password = expandEnvVar(c.Auth.Password)
	}
	if strings.HasPrefix(c.Auth.ClientID, "$") {
		c.Auth.ClientID = expandEnvVar(c.Auth.ClientID)
	}
	if strings.HasPrefix(c.Auth.ClientSecret, "$") {
		c.Auth.ClientSecret = expandEnvVar(c.Auth.ClientSecret)
	}
	if strings.HasPrefix(c.Auth.ServiceAccountJSON, "$") {
		c.Auth.ServiceAccountJSON = expandEnvVar(c.Auth.ServiceAccountJSON)
	}
}

// GetOptionString returns a string option value
func (c *Config) GetOptionString(key string, defaultValue string) string {
	if val, ok := c.Options[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}

// GetOptionBool returns a boolean option value
func (c *Config) GetOptionBool(key string, defaultValue bool) bool {
	if val, ok := c.Options[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return defaultValue
}

// GetOptionInt returns an integer option value
func (c *Config) GetOptionInt(key string, defaultValue int) int {
	if val, ok := c.Options[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}
	return defaultValue
}

// Clone creates a deep copy of the config
func (c *Config) Clone() *Config {
	clone := *c

	// Clone options map
	if c.Options != nil {
		clone.Options = make(map[string]interface{})
		for k, v := range c.Options {
			clone.Options[k] = v
		}
	}

	// Clone logging config
	if c.Logging != nil {
		loggingClone := *c.Logging
		clone.Logging = &loggingClone
	}

	return &clone
}

// Configs represents multiple bot configurations
type Configs struct {
	Bots    []*Config      `json:"bots" yaml:"bots"`
	Logging *LoggingConfig `json:"logging,omitempty" yaml:"logging,omitempty"`
	Manager *ManagerConfig `json:"manager,omitempty" yaml:"manager,omitempty"`
}

// Validate validates all configurations
func (cs *Configs) Validate() error {
	for i, cfg := range cs.Bots {
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("bot %d: %w", i, err)
		}
	}
	return nil
}

// ExpandEnvVars expands environment variables in all configurations
func (cs *Configs) ExpandEnvVars() {
	for _, cfg := range cs.Bots {
		cfg.ExpandEnvVars()
	}
}

// GetEnabledConfigs returns only enabled configurations
func (cs *Configs) GetEnabledConfigs() []*Config {
	var enabled []*Config
	for _, cfg := range cs.Bots {
		if cfg.Enabled {
			enabled = append(enabled, cfg)
		}
	}
	return enabled
}
