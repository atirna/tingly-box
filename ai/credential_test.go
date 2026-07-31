package ai

import "testing"

func TestValidateCredential(t *testing.T) {
	tests := []struct {
		name    string
		auth    AuthType
		fields  map[string]string
		wantErr bool
	}{
		{
			name:   "non-multi-field is a no-op",
			auth:   AuthTypeAPIKey,
			fields: nil,
		},
		{
			name: "aws static keys ok",
			auth: AuthTypeAWSSigV4,
			fields: map[string]string{
				CredFieldAWSRegion:          "us-east-1",
				CredFieldAWSAccessKeyID:     "AKIA",
				CredFieldAWSSecretAccessKey: "secret",
			},
		},
		{
			name: "aws bearer token ok without keys",
			auth: AuthTypeAWSSigV4,
			fields: map[string]string{
				CredFieldAWSRegion:      "us-east-1",
				CredFieldAWSBearerToken: "bedrock-key",
			},
		},
		{
			name:    "aws missing region",
			auth:    AuthTypeAWSSigV4,
			fields:  map[string]string{CredFieldAWSAccessKeyID: "AKIA", CredFieldAWSSecretAccessKey: "s"},
			wantErr: true,
		},
		{
			name:    "aws missing both keys and bearer",
			auth:    AuthTypeAWSSigV4,
			fields:  map[string]string{CredFieldAWSRegion: "us-east-1"},
			wantErr: true,
		},
		{
			name:    "aws secret-only is incomplete",
			auth:    AuthTypeAWSSigV4,
			fields:  map[string]string{CredFieldAWSRegion: "us-east-1", CredFieldAWSAccessKeyID: "AKIA"},
			wantErr: true,
		},
		{
			name: "gcp complete",
			auth: AuthTypeGCPVertex,
			fields: map[string]string{
				CredFieldGCPProjectID:          "proj",
				CredFieldGCPLocation:           "us-east5",
				CredFieldGCPServiceAccountJSON: `{"type":"service_account"}`,
			},
		},
		{
			name:    "gcp missing project",
			auth:    AuthTypeGCPVertex,
			fields:  map[string]string{CredFieldGCPLocation: "us-east5", CredFieldGCPServiceAccountJSON: "{}"},
			wantErr: true,
		},
		{
			name: "azure complete",
			auth: AuthTypeAzureKey,
			fields: map[string]string{
				CredFieldAzureEndpoint:   "https://x.openai.azure.com",
				CredFieldAzureAPIVersion: "2024-10-21",
				CredFieldAzureAPIKey:     "key",
			},
		},
		{
			name:    "azure missing api version",
			auth:    AuthTypeAzureKey,
			fields:  map[string]string{CredFieldAzureEndpoint: "https://x", CredFieldAzureAPIKey: "key"},
			wantErr: true,
		},
		{
			name: "gcp malformed sa json",
			auth: AuthTypeGCPVertex,
			fields: map[string]string{
				CredFieldGCPProjectID:          "proj",
				CredFieldGCPLocation:           "us-east5",
				CredFieldGCPServiceAccountJSON: "{not json",
			},
			wantErr: true,
		},
		{
			name: "azure endpoint without scheme",
			auth: AuthTypeAzureKey,
			fields: map[string]string{
				CredFieldAzureEndpoint:   "my-res.openai.azure.com",
				CredFieldAzureAPIVersion: "2024-10-21",
				CredFieldAzureAPIKey:     "key",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCredential(tt.auth, tt.fields)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateCredential() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsSecretCredentialField(t *testing.T) {
	cases := []struct {
		auth AuthType
		key  string
		want bool
	}{
		{AuthTypeAWSSigV4, CredFieldAWSRegion, false},
		{AuthTypeAWSSigV4, CredFieldAWSAccessKeyID, false},
		{AuthTypeAWSSigV4, CredFieldAWSSecretAccessKey, true},
		{AuthTypeAWSSigV4, CredFieldAWSSessionToken, true},
		{AuthTypeAWSSigV4, CredFieldAWSBearerToken, true},
		{AuthTypeGCPVertex, CredFieldGCPProjectID, false},
		{AuthTypeGCPVertex, CredFieldGCPServiceAccountJSON, true},
		{AuthTypeAzureKey, CredFieldAzureEndpoint, false},
		{AuthTypeAzureKey, CredFieldAzureAPIKey, true},
		// Unknown key fails closed (treated as secret).
		{AuthTypeAWSSigV4, "unknown_field", true},
	}
	for _, c := range cases {
		if got := IsSecretCredentialField(c.auth, c.key); got != c.want {
			t.Errorf("IsSecretCredentialField(%s, %q) = %v, want %v", c.auth, c.key, got, c.want)
		}
	}
}

func TestValidateCredentialAPIStyle(t *testing.T) {
	cases := []struct {
		auth    AuthType
		style   string
		wantErr bool
	}{
		{AuthTypeAWSSigV4, "anthropic", false},
		{AuthTypeAWSSigV4, "openai", true},
		{AuthTypeGCPVertex, "anthropic", false},
		{AuthTypeGCPVertex, "google", false},
		{AuthTypeGCPVertex, "openai", true},
		{AuthTypeAzureKey, "openai", false},
		{AuthTypeAzureKey, "anthropic", true},
		{AuthTypeAPIKey, "anything", false}, // unrestricted
	}
	for _, c := range cases {
		err := ValidateCredentialAPIStyle(c.auth, c.style)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateCredentialAPIStyle(%s, %s) error = %v, wantErr %v", c.auth, c.style, err, c.wantErr)
		}
	}
}

func TestNormalizeCredential(t *testing.T) {
	got := NormalizeCredential(map[string]string{
		"region":  " us-east-1\n",
		"empty":   "   ",
		" spaced": "v",
	})
	want := map[string]string{"region": "us-east-1", "spaced": "v"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeCredential = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("NormalizeCredential[%q] = %q, want %q", k, got[k], v)
		}
	}
}
