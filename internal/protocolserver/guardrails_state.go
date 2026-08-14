package protocolserver

import (
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/guardrails"
	guardrailscore "github.com/tingly-dev/tingly-box/internal/guardrails/core"
	"github.com/tingly-dev/tingly-box/internal/server/config"
)

// GuardrailsState is the single source of truth for the guardrails runtime
// pointer: the mutex-guarded swap/read primitives, and the lifecycle
// operations (activation refresh, credential cache refresh) driven by config
// edits, hot-reload, and server construction. It is owned by protocolserver
// (the gateway evaluates guardrails on live requests); the host server's
// admin handlers reach it through the narrow GuardrailsRuntime interface
// implemented by *Server via thin forwarding methods.
//
// The gateway-facing half — building the evaluation envelope and applying
// guardrails during a live model request — lives in guardrails_runtime_ai.go.
// Those functions take the current runtime snapshot as an explicit parameter
// (via Current() below) rather than reaching into shared state directly.
type GuardrailsState struct {
	cfg *config.Config

	runtime *guardrails.Guardrails
	mu      sync.RWMutex
}

// NewGuardrailsState builds the guardrails runtime holder for cfg. The
// runtime pointer starts nil; call Set (or the host's init path) to activate.
func NewGuardrailsState(cfg *config.Config) *GuardrailsState {
	return &GuardrailsState{cfg: cfg}
}

// Current returns the active runtime snapshot (nil when guardrails are off).
func (g *GuardrailsState) Current() *guardrails.Guardrails {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	runtime := g.runtime
	g.mu.RUnlock()
	return runtime
}

func (g *GuardrailsState) SetRef(runtime *guardrails.Guardrails) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.runtime = runtime
	g.mu.Unlock()
}

func cloneGuardrailsRuntime(src *guardrails.Guardrails) *guardrails.Guardrails {
	if src == nil {
		return nil
	}
	cloned := &guardrails.Guardrails{}
	cloned.SetPolicyEngine(src.PolicyEngine())
	cloned.SetHistoryStore(src.HistoryStore())
	cloned.SetCredentialCache(src.CredentialCacheSnapshot())
	cloned.SetActivation(src.ConfigSnapshot(), src.IsActive())
	return cloned
}

// ----------------------------------------------------------------------
// Runtime Gate And Shared State
// ----------------------------------------------------------------------

// EnabledForScenario centralizes feature-flag checks so protocol handlers
// do not repeat scenario/global guardrails gating logic.
func (g *GuardrailsState) EnabledForScenario(scenario string) bool {
	if g == nil {
		return false
	}
	return GuardrailsEnabledForScenario(g.cfg, g.Current(), scenario)
}

// SupportedScenarios returns a copy of the scenarios guardrails can gate.
func (g *GuardrailsState) SupportedScenarios() []string {
	out := make([]string, len(GuardrailsSupportedScenarios))
	copy(out, GuardrailsSupportedScenarios)
	return out
}

func hasActiveGuardrailsPolicies(cfg guardrailscore.Config) bool {
	if len(cfg.Policies) == 0 || len(cfg.Groups) == 0 {
		return false
	}

	enabledGroups := make(map[string]struct{}, len(cfg.Groups))
	for _, group := range cfg.Groups {
		if !group.Enabled {
			continue
		}
		enabledGroups[group.ID] = struct{}{}
	}
	if len(enabledGroups) == 0 {
		return false
	}

	for _, policy := range cfg.Policies {
		if !policy.Enabled {
			continue
		}
		for _, groupID := range policy.Groups {
			if _, ok := enabledGroups[groupID]; ok {
				return true
			}
		}
	}
	return false
}

// Credential cache and activation state live alongside the runtime gate because
// they are shared by request masking, history rendering, and runtime reloads.
func (g *GuardrailsState) RefreshCredentialCache() error {
	runtime := g.Current()
	if runtime == nil {
		return nil
	}
	if g.cfg == nil || g.cfg.ConfigDir == "" {
		next := cloneGuardrailsRuntime(runtime)
		next.SetCredentialCache(guardrails.NewCredentialCache())
		g.SetRef(next)
		return nil
	}

	store, err := g.cfg.CredentialStore()
	if err != nil {
		return err
	}
	credentials, err := store.List()
	if err != nil {
		return err
	}
	built := guardrails.BuildCredentialCache(credentials, g.SupportedScenarios())
	next := cloneGuardrailsRuntime(runtime)
	next.SetCredentialCache(built)
	g.SetRef(next)
	return nil
}

func (g *GuardrailsState) RefreshCredentialCacheOrWarn(context string) {
	if err := g.RefreshCredentialCache(); err != nil {
		logrus.WithError(err).Warnf("Guardrails credential cache refresh failed after %s", context)
	}
}

func (g *GuardrailsState) refreshActivationState() {
	runtime := g.Current()
	if runtime == nil {
		return
	}

	nextCfg := guardrailscore.Config{}
	nextActive := false
	if g.cfg == nil || g.cfg.ConfigDir == "" {
		next := cloneGuardrailsRuntime(runtime)
		next.SetActivation(nextCfg, nextActive)
		g.SetRef(next)
		return
	}

	cfgPath, err := config.FindConfig(g.cfg.ConfigDir)
	if err != nil {
		return
	}

	cfg, err := guardrails.LoadConfig(cfgPath)
	if err != nil {
		logrus.WithError(err).Debug("Guardrails activation state: failed to load config")
		next := cloneGuardrailsRuntime(runtime)
		next.SetActivation(nextCfg, nextActive)
		g.SetRef(next)
		return
	}
	nextCfg = cfg
	nextActive = hasActiveGuardrailsPolicies(cfg)
	next := cloneGuardrailsRuntime(runtime)
	next.SetActivation(nextCfg, nextActive)
	g.SetRef(next)
}

// Set swaps in a new runtime, carrying over the previous history store and
// credential cache when the incoming runtime lacks them, then refreshes
// activation and credential state. context labels warning logs.
func (g *GuardrailsState) Set(runtime *guardrails.Guardrails, context string) {
	prev := g.Current()
	if runtime != nil && prev != nil {
		if runtime.HistoryStore() == nil {
			runtime.SetHistoryStore(prev.HistoryStore())
		}
		cache := runtime.CredentialCacheSnapshot()
		if len(cache.ByID) == 0 && len(cache.ByScenario) == 0 {
			runtime.SetCredentialCache(prev.CredentialCacheSnapshot())
		}
	}
	g.SetRef(runtime)
	if runtime != nil {
		g.refreshActivationState()
		g.RefreshCredentialCacheOrWarn(context)
	}
}
