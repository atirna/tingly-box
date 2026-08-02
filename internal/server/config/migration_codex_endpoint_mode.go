package config

import (
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/ai"
)

// migrate20260518 backfills the Responses endpoint declaration on existing
// Codex OAuth providers. Codex's API only exposes /responses (no
// /chat/completions); the declaration is set on Codex providers at OAuth
// instantiation, but existing user configs from before this change don't
// carry it. Without the backfill, the resolver's default-Chat semantics
// would silently send /chat/completions requests to Codex and fail.
//
// Originally written against the legacy openai_endpoint_mode enum; now
// expressed in the split OpenAIEndpoints declaration (the store converts
// legacy rows on read, so this stays idempotent across both formats).
func migrate20260518(c *Config) {
	// Providers live in SQLite (the JSON c.Providers slice is legacy backup).
	// Backfill the DB-stored ones directly so the resolver sees the declaration.
	if c.providerStore != nil {
		if oauthProviders, err := c.providerStore.ListOAuth(); err == nil {
			for _, p := range oauthProviders {
				if p.OAuthDetail == nil || p.OAuthDetail.GetIssuer() != ai.IssuerCodex {
					continue
				}
				if p.SupportsOpenAIEndpoint(ai.OpenAIEndpointResponses) {
					continue
				}
				p.OpenAIEndpoints = ai.OpenAIEndpointsForIssuer(ai.IssuerCodex)
				if err := c.providerStore.Save(p); err != nil {
					logrus.WithError(err).WithField("provider_uuid", p.UUID).Warn("Failed to backfill openai_endpoints on Codex provider")
					continue
				}
				logrus.WithField("provider_uuid", p.UUID).Info("Backfilled openai_endpoints=[responses] on Codex provider (db)")
			}
		} else {
			logrus.WithError(err).Warn("Failed to list OAuth providers for openai_endpoints backfill")
		}
	}
}
