package command

import (
	"fmt"

	"github.com/tingly-dev/tingly-box/internal/dataio"
	serverconfig "github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// ImportOptions controls provider import behavior.
type ImportOptions struct {
	// Quiet suppresses progress output.
	Quiet bool
}

// ProviderImportInfo describes one imported provider.
type ProviderImportInfo struct {
	UUID    string
	Name    string
	Action  string
	Renamed bool
}

// ImportResult contains the results of a provider import.
type ImportResult struct {
	ProvidersCreated int
	Providers        []ProviderImportInfo
	ProviderMap      map[string]string
}

// collectProvidersFromRule returns the configured providers referenced by a
// rule. Missing provider references are ignored, matching the previous export
// behavior.
func collectProvidersFromRule(cfg *serverconfig.Config, rule *typ.Rule) []*typ.Provider {
	providerUUIDs := providerUUIDsFromRule(rule)
	providers := make([]*typ.Provider, 0, len(providerUUIDs))
	for _, providerUUID := range providerUUIDs {
		provider, err := cfg.GetProviderByUUID(providerUUID)
		if err == nil && provider != nil {
			providers = append(providers, provider)
		}
	}
	return providers
}

func exportProviders(providers []*typ.Provider, format dataio.Format) (string, error) {
	if len(providers) == 0 {
		return "", fmt.Errorf("providers must be specified for export")
	}

	result, err := dataio.Export(&dataio.ExportRequest{Providers: providers}, format)
	if err != nil {
		return "", fmt.Errorf("failed to export: %w", err)
	}
	return result.Content, nil
}

func providerUUIDsFromRule(rule *typ.Rule) []string {
	uuids := make(map[string]struct{})
	for _, service := range rule.Services {
		if service.Provider != "" {
			uuids[service.Provider] = struct{}{}
		}
	}

	result := make([]string, 0, len(uuids))
	for uuid := range uuids {
		result = append(result, uuid)
	}
	return result
}

// ImportProviders imports provider data without requiring an AppManager.
func ImportProviders(cfg *serverconfig.Config, data string, format dataio.Format, opts ImportOptions) (*ImportResult, error) {
	result, err := dataio.Import(data, cfg, format, dataio.ImportOptions{
		Quiet: opts.Quiet,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to import providers: %w", err)
	}

	providers := make([]ProviderImportInfo, len(result.Providers))
	for i, provider := range result.Providers {
		providers[i] = ProviderImportInfo{
			UUID: provider.UUID, Name: provider.Name, Action: provider.Action, Renamed: provider.Renamed,
		}
	}
	return &ImportResult{
		ProvidersCreated: result.ProvidersCreated,
		Providers:        providers,
		ProviderMap:      result.ProviderMap,
	}, nil
}
