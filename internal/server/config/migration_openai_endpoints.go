package config

import (
	"github.com/sirupsen/logrus"
)

// migrate20260802 converges provider rows onto the split OpenAIEndpoints
// declaration. Rows written before the change carry only the legacy
// openai_endpoint_mode enum column; the store already converts them on read
// ("" → undeclared, "chat"/"responses"/"both" → the equivalent endpoint
// list), so re-saving each such provider persists the converted form and
// clears the legacy column. Pure format conversion — routing behavior is
// unchanged for every value.
//
// Deliberately NOT a capability backfill: providers with an empty
// declaration stay empty (per the accepted spec, existing deepseek/openai
// providers do not gain a responses declaration automatically — users
// enable it via the provider-edit checkbox).
//
// Idempotent: converged rows have an empty legacy column and are skipped.
func migrate20260802(c *Config) {
	if c.providerStore == nil {
		return
	}
	providers, err := c.providerStore.List()
	if err != nil {
		logrus.WithError(err).Warn("Failed to list providers for openai_endpoints format conversion")
		return
	}
	for _, p := range providers {
		legacy, err := c.providerStore.HasLegacyEndpointMode(p.UUID)
		if err != nil || !legacy {
			continue
		}
		if err := c.providerStore.Save(p); err != nil {
			logrus.WithError(err).WithField("provider_uuid", p.UUID).Warn("Failed to converge openai_endpoints declaration")
			continue
		}
		logrus.WithFields(logrus.Fields{
			"provider_uuid":    p.UUID,
			"openai_endpoints": p.OpenAIEndpoints,
		}).Info("Converged legacy openai_endpoint_mode to openai_endpoints")
	}
}
