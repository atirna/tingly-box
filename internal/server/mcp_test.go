package server

import (
	"testing"

	"github.com/tingly-dev/tingly-box/internal/protocolserver"

	"github.com/stretchr/testify/assert"
	"github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestAnthropicBetaGenericPathUsesProviderLimits(t *testing.T) {
	s := &Server{config: &config.Config{}}

	provider := &typ.Provider{Name: "deepseek"}
	assert.True(t, protocolserver.ShouldUseGenericMCPForProvider(s.config, provider))

	s.config.GenericMCP.ProviderLimits = "other-provider"
	assert.False(t, protocolserver.ShouldUseGenericMCPForProvider(s.config, provider))
}
