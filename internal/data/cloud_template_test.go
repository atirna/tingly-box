package data

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// TestCloudTemplatesResolveModels verifies the embedded cloud provider templates
// (Bedrock / Vertex-Claude / Vertex-Gemini / Azure) match a provider built the
// way CloudProviderDialog builds it — by the credential-derived api_base + the
// declared api_style — and return their seeded model lists. It also proves the
// two Vertex templates (same canonical_domain) are disambiguated by api_style.
func TestCloudTemplatesResolveModels(t *testing.T) {
	tm := NewEmbeddedOnlyTemplateManager()
	if err := tm.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cases := []struct {
		name      string
		apiBase   string
		apiStyle  protocol.APIStyle
		authType  typ.AuthType
		wantModel string // one model that must be present
	}{
		{
			name:      "bedrock",
			apiBase:   "https://bedrock-runtime.us-east-1.amazonaws.com",
			apiStyle:  protocol.APIStyleAnthropic,
			authType:  typ.AuthTypeAWSSigV4,
			wantModel: "anthropic.claude-opus-4-8",
		},
		{
			name:      "vertex-claude",
			apiBase:   "https://us-east5-aiplatform.googleapis.com",
			apiStyle:  protocol.APIStyleAnthropic,
			authType:  typ.AuthTypeGCPVertex,
			wantModel: "claude-opus-4-8",
		},
		{
			name:      "vertex-gemini",
			apiBase:   "https://us-central1-aiplatform.googleapis.com",
			apiStyle:  protocol.APIStyleGoogle,
			authType:  typ.AuthTypeGCPVertex,
			wantModel: "gemini-2.5-pro",
		},
		{
			name:      "azure",
			apiBase:   "https://my-res.openai.azure.com",
			apiStyle:  protocol.APIStyleOpenAI,
			authType:  typ.AuthTypeAzureKey,
			wantModel: "gpt-4o",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &typ.Provider{
				Name:     tc.name,
				APIBase:  tc.apiBase,
				APIStyle: tc.apiStyle,
				AuthType: tc.authType,
			}
			models, _, err := tm.GetModelsForProvider(p)
			if err != nil {
				t.Fatalf("GetModelsForProvider: %v", err)
			}
			if !slices.Contains(models, tc.wantModel) {
				t.Errorf("models %v do not contain %q", models, tc.wantModel)
			}
		})
	}
}

// TestVertexDisambiguationByStyle guards the specific failure mode: the two
// Vertex templates share canonical_domain "aiplatform.googleapis.com", so
// without api_style matching the wrong model family could be returned.
func TestVertexDisambiguationByStyle(t *testing.T) {
	tm := NewEmbeddedOnlyTemplateManager()
	if err := tm.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	claude := &typ.Provider{APIBase: "https://us-east5-aiplatform.googleapis.com", APIStyle: protocol.APIStyleAnthropic, AuthType: typ.AuthTypeGCPVertex}
	gemini := &typ.Provider{APIBase: "https://us-east5-aiplatform.googleapis.com", APIStyle: protocol.APIStyleGoogle, AuthType: typ.AuthTypeGCPVertex}

	claudeModels, _, err := tm.GetModelsForProvider(claude)
	if err != nil {
		t.Fatalf("claude: %v", err)
	}
	geminiModels, _, err := tm.GetModelsForProvider(gemini)
	if err != nil {
		t.Fatalf("gemini: %v", err)
	}

	if !slices.Contains(claudeModels, "claude-opus-4-8") || slices.Contains(claudeModels, "gemini-2.5-pro") {
		t.Errorf("anthropic-style Vertex resolved wrong family: %v", claudeModels)
	}
	if !slices.Contains(geminiModels, "gemini-2.5-pro") || slices.Contains(geminiModels, "claude-opus-4-8") {
		t.Errorf("google-style Vertex resolved wrong family: %v", geminiModels)
	}
}

// TestExternalRegistryKeepsEmbeddedOnlyTemplates guards the failure mode that
// hid the Cloud picker section: a remote registry (or its disk cache) that
// predates the cloud templates used to replace tm.templates wholesale, so the
// /provider-templates endpoint served no cloud entries and the frontend had
// nothing to render. External entries must still win on id collision, but
// embedded-only ids must survive the swap.
func TestExternalRegistryKeepsEmbeddedOnlyTemplates(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	remote := `{
		"version": "remote-1",
		"providers": {
			"remote-only": {"id": "remote-only", "name": "Remote Only", "base_url_openai": "https://remote.example.com/v1"},
			"aws-bedrock": {"id": "aws-bedrock", "name": "Remote Bedrock Override", "auth_type": "aws_sigv4"}
		}
	}`
	if err := os.WriteFile(registryPath, []byte(remote), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	tm := NewTemplateManager("file://" + registryPath)
	if err := tm.loadEmbeddedTemplates(); err != nil {
		t.Fatalf("loadEmbeddedTemplates: %v", err)
	}
	registry, err := tm.FetchTemplates(context.Background())
	if err != nil {
		t.Fatalf("FetchTemplates: %v", err)
	}

	all := tm.GetAllTemplates()
	// The remote set is authoritative for ids it carries...
	if got := all["aws-bedrock"]; got == nil || got.Name != "Remote Bedrock Override" {
		t.Errorf("external template should win on id collision, got %+v", got)
	}
	if all["remote-only"] == nil {
		t.Error("remote-only template missing after fetch")
	}
	// ...but embedded-only ids (the other cloud presets) must stay visible,
	// both in GetAllTemplates and in the registry the refresh endpoint returns.
	for _, id := range []string{"gcp-vertex-claude", "gcp-vertex-gemini", "azure-openai"} {
		if all[id] == nil {
			t.Errorf("embedded-only template %q hidden by external registry", id)
		}
		if registry.Providers[id] == nil {
			t.Errorf("embedded-only template %q missing from fetched registry response", id)
		}
	}
}

