package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestMatchRule_Context1MNormalized(t *testing.T) {
	c := &Config{
		Rules: []typ.Rule{
			// Desktop rule renamed by the 1M toggle.
			{UUID: "d1", Scenario: typ.ScenarioClaudeDesktop, RequestModel: "claude-sonnet-4-6[1m]"},
			// Bare CC profile rule; client env may advertise "haiku[1m]".
			{UUID: "p1", Scenario: typ.RuleScenario("claude_code:p1"), RequestModel: "haiku"},
			// Non-Claude scenario must NOT be normalized.
			{UUID: "o1", Scenario: typ.ScenarioOpenAI, RequestModel: "gpt-4o"},
		},
	}

	// Stale Desktop config sends the bare name → matches the renamed rule.
	if r := c.MatchRuleByModelAndScenario("claude-sonnet-4-6", typ.ScenarioClaudeDesktop); r == nil || r.UUID != "d1" {
		t.Errorf("bare name should match [1m]-renamed desktop rule, got %+v", r)
	}
	// Suffixed pick from /v1/models matches exactly (fast path).
	if r := c.MatchRuleByModelAndScenario("claude-sonnet-4-6[1m]", typ.ScenarioClaudeDesktop); r == nil || r.UUID != "d1" {
		t.Errorf("suffixed name should match renamed desktop rule, got %+v", r)
	}
	// Suffixed request against a bare profile rule (profiled scenario → base
	// scenario is claude_code, so normalization applies).
	if r := c.MatchRuleByModelAndScenario("haiku[1m]", typ.RuleScenario("claude_code:p1")); r == nil || r.UUID != "p1" {
		t.Errorf("suffixed request should match bare profile rule, got %+v", r)
	}
	// Non-Claude scenarios keep strict matching.
	if r := c.MatchRuleByModelAndScenario("gpt-4o[1m]", typ.ScenarioOpenAI); r != nil {
		t.Errorf("openai scenario must not 1m-normalize, got %+v", r)
	}
}

func TestUpdateRule_DesktopSyncsContext1MName(t *testing.T) {
	c := &Config{
		ConfigFile: filepath.Join(t.TempDir(), "config.json"),
		Rules: []typ.Rule{
			{UUID: "d1", Scenario: typ.ScenarioClaudeDesktop, RequestModel: "claude-sonnet-4-6", Active: true},
			{UUID: "cc1", Scenario: typ.ScenarioClaudeCode, RequestModel: "tingly/cc-haiku", Active: true},
		},
	}

	// Enabling the flag renames the desktop rule so /v1/models lists [1m].
	r := c.Rules[0]
	r.Flags.Context1M = true
	if err := c.UpdateRule("d1", r); err != nil {
		t.Fatalf("UpdateRule error: %v", err)
	}
	if got := c.Rules[0].RequestModel; got != "claude-sonnet-4-6[1m]" {
		t.Errorf("desktop rule should be renamed with [1m], got %q", got)
	}

	// Disabling the flag strips the suffix again.
	r = c.Rules[0]
	r.Flags.Context1M = false
	if err := c.UpdateRule("d1", r); err != nil {
		t.Fatalf("UpdateRule error: %v", err)
	}
	if got := c.Rules[0].RequestModel; got != "claude-sonnet-4-6" {
		t.Errorf("desktop rule should be stripped back, got %q", got)
	}

	// Claude Code rules are not renamed — their [1m] travels via the env.
	r = c.Rules[1]
	r.Flags.Context1M = true
	if err := c.UpdateRule("cc1", r); err != nil {
		t.Fatalf("UpdateRule error: %v", err)
	}
	if got := c.Rules[1].RequestModel; got != "tingly/cc-haiku" {
		t.Errorf("claude_code rule must keep its bare name, got %q", got)
	}
}

func TestAddRule_DuplicateNameSameScenario(t *testing.T) {
	cfg, err := NewConfig(WithConfigDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewConfig error: %v", err)
	}

	rule1 := typ.Rule{
		UUID:         "uuid-1",
		Scenario:     "openai",
		RequestModel: "gpt-4",
	}
	if err := cfg.AddRule(rule1); err != nil {
		t.Fatalf("first AddRule failed: %v", err)
	}

	rule2 := typ.Rule{
		UUID:         "uuid-2",
		Scenario:     "openai",
		RequestModel: "gpt-4",
	}
	err = cfg.AddRule(rule2)
	if err == nil {
		t.Fatal("expected error for duplicate name in same scenario, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAddRule_DuplicateNameDifferentScenario(t *testing.T) {
	cfg, err := NewConfig(WithConfigDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewConfig error: %v", err)
	}

	rule1 := typ.Rule{
		UUID:         "uuid-1",
		Scenario:     "openai",
		RequestModel: "gpt-4",
	}
	if err := cfg.AddRule(rule1); err != nil {
		t.Fatalf("first AddRule failed: %v", err)
	}

	// Same request_model but different scenario — must succeed
	rule2 := typ.Rule{
		UUID:         "uuid-2",
		Scenario:     "anthropic",
		RequestModel: "gpt-4",
	}
	if err := cfg.AddRule(rule2); err != nil {
		t.Errorf("AddRule with same name in different scenario should succeed, got: %v", err)
	}
}

func TestAddRule_TeamSeedsCreateDefaults(t *testing.T) {
	cfg, err := NewConfig(WithConfigDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewConfig error: %v", err)
	}

	// A bare team rule (no flags) — as any non-HTTP path (CLI/TUI/import) would
	// build it — must come out with the team creation defaults seeded on.
	if err := cfg.AddRule(typ.Rule{UUID: "team-1", Scenario: typ.ScenarioTeam, RequestModel: "m"}); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}
	seeded := cfg.GetRuleByUUID("team-1")
	if seeded == nil {
		t.Fatal("team rule not found after AddRule")
	}
	if !seeded.Flags.ClaudeCodeCompat || !seeded.Flags.CleanHeader {
		t.Errorf("expected team defaults seeded, got %+v", seeded.Flags)
	}

	// An explicit flag set is left untouched — the defaults are not layered on.
	if err := cfg.AddRule(typ.Rule{UUID: "team-2", Scenario: typ.ScenarioTeam, RequestModel: "m2", Flags: typ.RuleFlags{SkipUsage: true}}); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}
	explicit := cfg.GetRuleByUUID("team-2")
	if explicit == nil {
		t.Fatal("explicit team rule not found after AddRule")
	}
	if !explicit.Flags.SkipUsage || explicit.Flags.ClaudeCodeCompat || explicit.Flags.CleanHeader {
		t.Errorf("explicit flags must not be overridden, got %+v", explicit.Flags)
	}

	// A non-team rule keeps its empty flag set.
	if err := cfg.AddRule(typ.Rule{UUID: "oai-1", Scenario: typ.ScenarioOpenAI, RequestModel: "m3"}); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}
	other := cfg.GetRuleByUUID("oai-1")
	if other == nil {
		t.Fatal("openai rule not found after AddRule")
	}
	if !other.Flags.IsZero() {
		t.Errorf("non-team rule should keep empty flags, got %+v", other.Flags)
	}
}

func TestAddRule_DuplicateUUID(t *testing.T) {
	cfg, err := NewConfig(WithConfigDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewConfig error: %v", err)
	}

	rule1 := typ.Rule{
		UUID:         "uuid-1",
		Scenario:     "openai",
		RequestModel: "gpt-4",
	}
	if err := cfg.AddRule(rule1); err != nil {
		t.Fatalf("first AddRule failed: %v", err)
	}

	rule2 := typ.Rule{
		UUID:         "uuid-1", // same UUID, different model
		Scenario:     "openai",
		RequestModel: "gpt-3.5-turbo",
	}
	if err := cfg.AddRule(rule2); err == nil {
		t.Fatal("expected error for duplicate UUID, got nil")
	}
}

// TestAddRule_ExtraHeadersValidation: the AddRule/UpdateRule choke points
// reject denied header names and canonicalize accepted ones, sharing the same
// gate as provider/model-level headers.
func TestAddRule_ExtraHeadersValidation(t *testing.T) {
	cfg, err := NewConfig(WithConfigDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewConfig error: %v", err)
	}

	denied := typ.Rule{
		UUID:         "uuid-hdr-1",
		Scenario:     "openai",
		RequestModel: "hdr-model",
		Flags:        typ.RuleFlags{ExtraHeaders: map[string]string{"Authorization": "Bearer x"}},
	}
	if err := cfg.AddRule(denied); err == nil {
		t.Fatal("expected denied header to be rejected, got nil")
	}

	ok := typ.Rule{
		UUID:         "uuid-hdr-2",
		Scenario:     "openai",
		RequestModel: "hdr-model-2",
		Flags:        typ.RuleFlags{ExtraHeaders: map[string]string{"x-title": "tingly"}},
	}
	if err := cfg.AddRule(ok); err != nil {
		t.Fatalf("valid header rejected: %v", err)
	}
	saved := cfg.GetRuleByUUID("uuid-hdr-2")
	if saved == nil {
		t.Fatal("rule not saved")
	}
	if _, exists := saved.Flags.ExtraHeaders["X-Title"]; !exists {
		t.Errorf("header name not canonicalized on save: %v", saved.Flags.ExtraHeaders)
	}

	// UpdateRule shares the gate.
	bad := *saved
	bad.Flags.ExtraHeaders = map[string]string{"user-agent": "x"}
	if err := cfg.UpdateRule(bad.UUID, bad); err == nil {
		t.Fatal("expected UpdateRule to reject denied header, got nil")
	}
}
