package agent

import "fmt"

// DshConfig implements AgentConfig for DeepSeek Harness (dsh)
type DshConfig struct{}

// DshParams contains parameters for applying DeepSeek Harness configuration
type DshParams struct {
	// DshBaseURL is the base URL dsh should route the tingly-box provider to.
	DshBaseURL string

	// APIKey is written into $DSH_HOME/.credentials.yaml under the env var
	// name settings.yaml references via apiKeyEnv.
	APIKey string

	// Models is the list of model ids for the tingly-box provider entry.
	// Caller is responsible for collecting and deduplicating these.
	Models []string

	// Prefs holds the typed, whitelisted, user-tunable settings.yaml keys
	// (see DshPrefs). nil means "use built-in defaults".
	Prefs *DshPrefs
}

// Apply applies DeepSeek Harness configuration
func (d *DshConfig) Apply(paramsInterface interface{}) (*ApplyAgentResult, error) {
	params, ok := paramsInterface.(*DshParams)
	if !ok {
		return nil, fmt.Errorf("invalid params type, expected *DshParams")
	}

	result := &ApplyAgentResult{
		AgentType: AgentTypeDsh,
		Success:   true,
	}

	settingsResult, err := ApplyDshSettings(params.DshBaseURL, params.Models, params.Prefs)
	if err != nil {
		return nil, fmt.Errorf("failed to apply DeepSeek Harness settings: %w", err)
	}
	result.Success = result.Success && settingsResult.Success
	if settingsResult.Success {
		suffix := " (updated)"
		if settingsResult.Created {
			suffix = " (created)"
		}
		result.ConfigFiles = append(result.ConfigFiles, "$DSH_HOME/settings.yaml"+suffix)
	}
	if settingsResult.BackupPath != "" {
		result.BackupPaths = append(result.BackupPaths, settingsResult.BackupPath)
	}

	credsResult, err := ApplyDshCredentials(params.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to apply DeepSeek Harness credentials: %w", err)
	}
	result.Success = result.Success && credsResult.Success
	if credsResult.Success {
		suffix := " (updated)"
		if credsResult.Created {
			suffix = " (created)"
		}
		result.ConfigFiles = append(result.ConfigFiles, "$DSH_HOME/.credentials.yaml"+suffix)
	}
	if credsResult.BackupPath != "" {
		result.BackupPaths = append(result.BackupPaths, credsResult.BackupPath)
	}

	return result, nil
}

// Restore restores DeepSeek Harness configuration from backup
func (d *DshConfig) Restore() (*RestoreAgentResult, error) {
	return RestoreAgent(AgentTypeDsh)
}
