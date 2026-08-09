// Package catalog is the per-vendor model capability catalog: what each model
// can do, declared once per model family, independent of which provider serves
// it. It complements internal/data/providers.json, which is the offering
// registry (who serves which model, at which endpoint, with what limits) —
// capability facts live here, deployment facts live there.
//
// Layout: one <vendor>.models.json data file plus one <vendor>.go loader per
// vendor (claude.models.json + claude.go today; openai/gemini can follow the
// same pattern). Each file only carries the fields its loader actually
// consumes — deliberately not a mirror of the vendor's full /v1/models
// response, whose unused fields (display names, dates, unrelated capability
// flags) are dead weight. The shape is inspired by OpenRouter's flat
// `reasoning: {supported_efforts: [...], ...}` block rather than Anthropic's
// nested `capabilities.effort.<level>.supported` tree. Update the JSON when
// new models land instead of hardcoding model names in code; the
// completeness test in this package fails when providers.json offers a
// Claude model this catalog does not describe.
package catalog

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed claude.models.json
var claudeModelsJSON []byte

// ClaudeThinkingCaps describes which thinking dialects a cataloged Claude
// model accepts.
type ClaudeThinkingCaps struct {
	// ThinkingEnabled: accepts thinking.type=enabled with budget_tokens.
	ThinkingEnabled bool
	// ThinkingAdaptive: accepts thinking.type=adaptive.
	ThinkingAdaptive bool
	// EffortLevels: supported output_config.effort values (e.g. "low", "max").
	// Empty means the model has no effort support.
	EffortLevels map[string]bool
}

type catalogModel struct {
	ID       string `json:"id"`
	Thinking struct {
		Budget   bool     `json:"budget"`
		Adaptive bool     `json:"adaptive"`
		Efforts  []string `json:"efforts"`
	} `json:"thinking"`
}

type claudeCapsEntry struct {
	key  string
	caps ClaudeThinkingCaps
}

var claudeCapsIndex = sync.OnceValue(buildClaudeCapsIndex)

var claudeDateSuffixRE = regexp.MustCompile(`-\d{8}$`)

// buildClaudeCapsIndex flattens the catalog into substring match keys: each
// model is indexed under its full id and its date-stripped family name, so
// bare names ("claude-opus-4-5"), dated ids, and cloud-provider decorations
// ("us.anthropic.claude-sonnet-4-5-20250929-v1:0") all resolve. Keys are
// sorted longest-first so the most specific entry wins (e.g.
// "claude-sonnet-4-6" before "claude-sonnet-4").
func buildClaudeCapsIndex() []claudeCapsEntry {
	var models []catalogModel
	if err := json.Unmarshal(claudeModelsJSON, &models); err != nil {
		return nil
	}

	var entries []claudeCapsEntry
	seen := map[string]bool{}
	add := func(key string, caps ClaudeThinkingCaps) {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		entries = append(entries, claudeCapsEntry{key: key, caps: caps})
	}

	for _, m := range models {
		caps := ClaudeThinkingCaps{
			ThinkingEnabled:  m.Thinking.Budget,
			ThinkingAdaptive: m.Thinking.Adaptive,
		}
		if len(m.Thinking.Efforts) > 0 {
			caps.EffortLevels = make(map[string]bool, len(m.Thinking.Efforts))
			for _, lvl := range m.Thinking.Efforts {
				caps.EffortLevels[lvl] = true
			}
		}
		add(m.ID, caps)
		add(claudeDateSuffixRE.ReplaceAllString(m.ID, ""), caps)
	}

	sort.SliceStable(entries, func(i, j int) bool { return len(entries[i].key) > len(entries[j].key) })
	return entries
}

// LookupClaudeThinkingCaps resolves a model name to the catalog's thinking
// capabilities. The longest catalog key contained in the (lowercased) name
// wins; ok=false when the model is not in the catalog.
func LookupClaudeThinkingCaps(model string) (ClaudeThinkingCaps, bool) {
	m := strings.ToLower(model)
	if m == "" {
		return ClaudeThinkingCaps{}, false
	}
	for _, e := range claudeCapsIndex() {
		if strings.Contains(m, e.key) {
			return e.caps, true
		}
	}
	return ClaudeThinkingCaps{}, false
}
