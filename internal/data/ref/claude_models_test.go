package ref

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupClaudeThinkingCaps(t *testing.T) {
	t.Run("exact dated id", func(t *testing.T) {
		caps, ok := LookupClaudeThinkingCaps("claude-opus-4-5-20251101")
		require.True(t, ok)
		assert.True(t, caps.ThinkingEnabled)
		assert.False(t, caps.ThinkingAdaptive)
		assert.True(t, caps.EffortLevels["high"])
		assert.False(t, caps.EffortLevels["max"])
	})

	t.Run("undated family name", func(t *testing.T) {
		caps, ok := LookupClaudeThinkingCaps("claude-sonnet-4-5")
		require.True(t, ok)
		assert.True(t, caps.ThinkingEnabled)
		assert.False(t, caps.ThinkingAdaptive)
		assert.Empty(t, caps.EffortLevels)
	})

	t.Run("cloud-provider decorations", func(t *testing.T) {
		caps, ok := LookupClaudeThinkingCaps("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
		require.True(t, ok)
		assert.True(t, caps.ThinkingEnabled)
	})

	t.Run("most specific key wins over family prefix", func(t *testing.T) {
		// "claude-sonnet-4-6" must not resolve to the "claude-sonnet-4" family.
		caps, ok := LookupClaudeThinkingCaps("claude-sonnet-4-6")
		require.True(t, ok)
		assert.True(t, caps.ThinkingAdaptive)
		assert.True(t, caps.EffortLevels["max"])

		caps, ok = LookupClaudeThinkingCaps("claude-sonnet-4-20250514")
		require.True(t, ok)
		assert.False(t, caps.ThinkingAdaptive)
		assert.Empty(t, caps.EffortLevels)
	})

	t.Run("adaptive-only opus 4.7", func(t *testing.T) {
		caps, ok := LookupClaudeThinkingCaps("claude-opus-4-7")
		require.True(t, ok)
		assert.False(t, caps.ThinkingEnabled)
		assert.True(t, caps.ThinkingAdaptive)
	})

	t.Run("no thinking at all", func(t *testing.T) {
		caps, ok := LookupClaudeThinkingCaps("claude-3-haiku-20240307")
		require.True(t, ok)
		assert.False(t, caps.ThinkingEnabled)
		assert.False(t, caps.ThinkingAdaptive)
		assert.Empty(t, caps.EffortLevels)
	})

	t.Run("unknown models miss", func(t *testing.T) {
		_, ok := LookupClaudeThinkingCaps("claude-3-5-sonnet-20241022")
		assert.False(t, ok, "3.5 sonnet is not in the catalog")
		_, ok = LookupClaudeThinkingCaps("gpt-5.2")
		assert.False(t, ok)
		_, ok = LookupClaudeThinkingCaps("")
		assert.False(t, ok)
	})
}
