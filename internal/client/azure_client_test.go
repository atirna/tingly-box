package client

import (
	"testing"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestAzureOptions(t *testing.T) {
	// valid → two options (endpoint + api key)
	opts, err := azureOptions(&typ.Provider{
		Name:     "azure",
		AuthType: typ.AuthTypeAzureKey,
		Credential: &typ.CredentialBundle{Fields: map[string]string{
			ai.CredFieldAzureEndpoint:   "https://x.openai.azure.com",
			ai.CredFieldAzureAPIVersion: "2024-10-21",
			ai.CredFieldAzureAPIKey:     "key",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 2 {
		t.Errorf("len(opts) = %d, want 2", len(opts))
	}

	// missing api version → error
	if _, err := azureOptions(&typ.Provider{
		Name:     "azure",
		AuthType: typ.AuthTypeAzureKey,
		Credential: &typ.CredentialBundle{Fields: map[string]string{
			ai.CredFieldAzureEndpoint: "https://x", ai.CredFieldAzureAPIKey: "key",
		}},
	}); err == nil {
		t.Error("expected error for missing api version")
	}
}
