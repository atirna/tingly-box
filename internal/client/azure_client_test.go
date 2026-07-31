package client

import (
	"testing"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// cloudProvider builds a minimal multi-field-credential provider for tests.
func cloudProvider(name string, authType typ.AuthType, fields map[string]string) *typ.Provider {
	return &typ.Provider{
		Name:       name,
		AuthType:   authType,
		Credential: &typ.CredentialBundle{Fields: fields},
	}
}

func TestAzureOptions(t *testing.T) {
	// valid → two options (endpoint + api key)
	opts, err := azureOptions(cloudProvider("azure", typ.AuthTypeAzureKey, map[string]string{
		ai.CredFieldAzureEndpoint:   "https://x.openai.azure.com",
		ai.CredFieldAzureAPIVersion: "2024-10-21",
		ai.CredFieldAzureAPIKey:     "key",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 2 {
		t.Errorf("len(opts) = %d, want 2", len(opts))
	}

	// missing api version → error
	if _, err := azureOptions(cloudProvider("azure", typ.AuthTypeAzureKey, map[string]string{
		ai.CredFieldAzureEndpoint: "https://x", ai.CredFieldAzureAPIKey: "key",
	})); err == nil {
		t.Error("expected error for missing api version")
	}
}
