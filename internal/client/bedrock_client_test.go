package client

import (
	"testing"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestAWSConfigFromBundle_StaticKeys(t *testing.T) {
	cfg := awsConfigFromBundle(&typ.CredentialBundle{Fields: map[string]string{
		ai.CredFieldAWSRegion:          "eu-west-1",
		ai.CredFieldAWSAccessKeyID:     "AKIA",
		ai.CredFieldAWSSecretAccessKey: "secret",
	}})
	if cfg.Region != "eu-west-1" {
		t.Errorf("Region = %q, want eu-west-1", cfg.Region)
	}
	if cfg.Credentials == nil {
		t.Error("expected static Credentials provider to be set")
	}
	if cfg.BearerAuthTokenProvider != nil {
		t.Error("did not expect a bearer token provider for static keys")
	}
}

func TestAWSConfigFromBundle_BearerPreferred(t *testing.T) {
	cfg := awsConfigFromBundle(&typ.CredentialBundle{Fields: map[string]string{
		ai.CredFieldAWSRegion:      "us-east-1",
		ai.CredFieldAWSBearerToken: "bedrock-key",
		// keys present but bearer must win
		ai.CredFieldAWSAccessKeyID:     "AKIA",
		ai.CredFieldAWSSecretAccessKey: "secret",
	}})
	if cfg.BearerAuthTokenProvider == nil {
		t.Error("expected bearer token provider to be preferred")
	}
	if cfg.Credentials != nil {
		t.Error("did not expect static Credentials when bearer token is present")
	}
}

func TestBedrockOption_ValidationErrors(t *testing.T) {
	// missing bundle
	if _, err := bedrockOption(&typ.Provider{Name: "b", AuthType: typ.AuthTypeAWSSigV4}); err == nil {
		t.Error("expected error for missing credential bundle")
	}
	// missing region
	if _, err := bedrockOption(cloudProvider("bedrock", typ.AuthTypeAWSSigV4, map[string]string{
		ai.CredFieldAWSAccessKeyID: "AKIA", ai.CredFieldAWSSecretAccessKey: "s",
	})); err == nil {
		t.Error("expected error for missing region")
	}
	// valid
	if _, err := bedrockOption(cloudProvider("bedrock", typ.AuthTypeAWSSigV4, map[string]string{
		ai.CredFieldAWSRegion:          "us-east-1",
		ai.CredFieldAWSAccessKeyID:     "AKIA",
		ai.CredFieldAWSSecretAccessKey: "s",
	})); err != nil {
		t.Errorf("unexpected error for valid bundle: %v", err)
	}
}
