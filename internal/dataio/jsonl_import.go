package dataio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// ImportOptions controls import behavior.
type ImportOptions struct {
	// Quiet suppresses progress output
	Quiet bool
}

// ProviderImportInfo contains information about an imported provider
type ProviderImportInfo struct {
	UUID   string
	Name   string
	Action string // "created"
	// Renamed is true when Name was auto-suffixed because it collided with
	// an already-existing provider name.
	Renamed bool
}

// ImportResult contains the results of an import operation
type ImportResult struct {
	ProvidersCreated int
	Providers        []ProviderImportInfo
	ProviderMap      map[string]string // old UUID -> new UUID
}

// Importer defines the interface for import implementations
type Importer interface {
	Import(data string, globalConfig *config.Config, opts ImportOptions) (*ImportResult, error)
	Format() Format
}

// JSONLImporter imports data from JSONL format
type JSONLImporter struct{}

// NewJSONLImporter creates a new JSONL importer
func NewJSONLImporter() *JSONLImporter {
	return &JSONLImporter{}
}

// Format returns the format type
func (i *JSONLImporter) Format() Format {
	return FormatJSONL
}

// Import imports data from JSONL format
func (i *JSONLImporter) Import(data string, globalConfig *config.Config, opts ImportOptions) (*ImportResult, error) {
	result := &ImportResult{
		ProviderMap: make(map[string]string),
	}

	// Parse lines
	scanner := bufio.NewScanner(strings.NewReader(data))
	var metadata *Metadata
	providersData := []*ProviderData{}

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue // Skip empty lines
		}

		// Parse line type
		var base DataLine
		if err := json.Unmarshal([]byte(line), &base); err != nil {
			return nil, fmt.Errorf("line %d: invalid JSON: %w", lineNum, err)
		}

		switch base.Type {
		case "metadata":
			if err := json.Unmarshal([]byte(line), &metadata); err != nil {
				return nil, fmt.Errorf("line %d: invalid metadata: %w", lineNum, err)
			}
			if metadata.Version != "1.0" {
				return nil, fmt.Errorf("unsupported export version: %s", metadata.Version)
			}

		case "provider":
			var provider ProviderData
			if err := json.Unmarshal([]byte(line), &provider); err != nil {
				return nil, fmt.Errorf("line %d: invalid provider data: %w", lineNum, err)
			}
			providersData = append(providersData, &provider)

		default:
			return nil, fmt.Errorf("line %d: unknown type '%s'", lineNum, base.Type)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading input: %w", err)
	}

	if len(providersData) == 0 {
		return nil, fmt.Errorf("no provider data found in export")
	}

	// Import providers
	for _, p := range providersData {
		info, err := i.importProvider(globalConfig, p, result.ProviderMap)
		if err != nil {
			return nil, fmt.Errorf("failed to import provider '%s': %w", p.Name, err)
		}
		result.ProvidersCreated++
		result.Providers = append(result.Providers, *info)
	}

	return result, nil
}

func (i *JSONLImporter) importProvider(globalConfig *config.Config, p *ProviderData, providerMap map[string]string) (*ProviderImportInfo, error) {
	// Check if provider name already exists (need to avoid duplicate names)
	_, err := globalConfig.GetProviderByName(p.Name)
	nameExists := err == nil

	// Always create a new UUID for imported providers
	// This allows the same provider export to be imported multiple times
	providerUUID := uuid.New().String()

	// If name exists, add suffix to avoid conflicts
	renamed := nameExists
	if nameExists {
		suffix := 2
		newName := fmt.Sprintf("%s-%d", p.Name, suffix)
		for {
			_, err := globalConfig.GetProviderByName(newName)
			if err != nil {
				break // Name is available
			}
			suffix++
			newName = fmt.Sprintf("%s-%d", p.Name, suffix)
		}
		p.Name = newName
	}

	// Create new provider with new UUID. Shallow-copy the embedded Provider so
	// we inherit every field automatically, then apply the deliberate
	// import-time overrides below.
	newProvider := p.Provider
	newProvider.UUID = providerUUID
	newProvider.Name = p.Name

	// Imported providers are always user-owned. Without this, an export that
	// happens to carry Source: "builtin" (e.g. from another instance's
	// support bundle) would create a provider that's permanently locked from
	// edit/delete via the API (see Provider.IsBuiltin gating in
	// internal/server/module/provider/handler.go).
	newProvider.Source = typ.ProviderSourceUser

	// LastUpdated is a freshness cache for Models, not portable data; reset it
	// so any staleness-driven refresh logic treats the import as needing a
	// fresh check rather than trusting a timestamp from the source instance.
	newProvider.LastUpdated = ""

	if err := globalConfig.AddProvider(&newProvider); err != nil {
		return nil, fmt.Errorf("failed to add provider: %w", err)
	}

	// Map old UUID to new UUID
	providerMap[p.UUID] = newProvider.UUID
	return &ProviderImportInfo{
		UUID:    newProvider.UUID,
		Name:    newProvider.Name,
		Action:  "created",
		Renamed: renamed,
	}, nil
}
