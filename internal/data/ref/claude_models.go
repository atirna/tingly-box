// Package ref exposes reference capability snapshots shipped with the binary.
// claude.models.json mirrors the Anthropic model catalog and is the single
// source of truth for per-model thinking/effort capability decisions — update
// that file when new models land instead of hardcoding model names in code.
package ref

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

type supportedFlag struct {
	Supported bool `json:"supported"`
}

// catalogEffort decodes the catalog's effort capability object, treating every
// key other than "supported" as a level flag so future levels need no code
// change.
type catalogEffort struct {
	Supported bool
	Levels    map[string]bool
}

func (e *catalogEffort) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.Levels = map[string]bool{}
	for key, val := range raw {
		if key == "supported" {
			if err := json.Unmarshal(val, &e.Supported); err != nil {
				return err
			}
			continue
		}
		var f supportedFlag
		if err := json.Unmarshal(val, &f); err == nil && f.Supported {
			e.Levels[key] = true
		}
	}
	return nil
}

type catalogModel struct {
	ID           string `json:"id"`
	Capabilities struct {
		Effort   catalogEffort `json:"effort"`
		Thinking struct {
			Supported bool `json:"supported"`
			Types     struct {
				Enabled  supportedFlag `json:"enabled"`
				Adaptive supportedFlag `json:"adaptive"`
			} `json:"types"`
		} `json:"thinking"`
	} `json:"capabilities"`
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
	var doc struct {
		Data []catalogModel `json:"data"`
	}
	if err := json.Unmarshal(claudeModelsJSON, &doc); err != nil {
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

	for _, m := range doc.Data {
		caps := ClaudeThinkingCaps{
			ThinkingEnabled:  m.Capabilities.Thinking.Supported && m.Capabilities.Thinking.Types.Enabled.Supported,
			ThinkingAdaptive: m.Capabilities.Thinking.Supported && m.Capabilities.Thinking.Types.Adaptive.Supported,
		}
		if m.Capabilities.Effort.Supported && len(m.Capabilities.Effort.Levels) > 0 {
			caps.EffortLevels = m.Capabilities.Effort.Levels
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
