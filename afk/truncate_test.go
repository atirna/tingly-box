package afk

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncate_UnderLimitsIsUnchanged(t *testing.T) {
	in := "one\ntwo\nthree"
	got := Truncate(in, TruncateOptions{})
	assert.False(t, got.Truncated)
	assert.Equal(t, in, got.Text)
	assert.Equal(t, in, got.String(), "String must not append a notice when nothing was dropped")
	assert.Equal(t, 3, got.TotalLines)
	assert.Equal(t, 3, got.KeptLines)
}

// A trailing newline terminates the last line rather than starting a new one.
func TestTruncate_TrailingNewlineIsNotACountedLine(t *testing.T) {
	got := Truncate("one\ntwo\n", TruncateOptions{})
	assert.Equal(t, 2, got.TotalLines)
	assert.False(t, got.Truncated)
}

func TestTruncate_LineCapKeepsHeadByDefault(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	got := Truncate(b.String(), TruncateOptions{MaxLines: 10})

	require.True(t, got.Truncated)
	assert.Equal(t, 100, got.TotalLines)
	assert.Equal(t, 10, got.KeptLines)
	assert.True(t, strings.HasPrefix(got.Text, "line-0\n"))
	assert.Contains(t, got.Text, "line-9")
	assert.NotContains(t, got.Text, "line-10")
	assert.Contains(t, got.String(), "first 10 of 100 lines")
}

func TestTruncate_LineCapKeepsTailWhenAsked(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	got := Truncate(b.String(), TruncateOptions{MaxLines: 10, KeepTail: true})

	require.True(t, got.Truncated)
	assert.Equal(t, 10, got.KeptLines)
	assert.True(t, strings.HasSuffix(got.Text, "line-99"))
	assert.NotContains(t, got.Text, "line-89")
	assert.Contains(t, got.String(), "last 10 of 100 lines")
}

// The byte cap has to bind independently of the line cap — a handful of very
// long lines is the case a line-only limit misses entirely.
func TestTruncate_ByteCapBindsIndependentlyOfLineCap(t *testing.T) {
	long := strings.Repeat("x", 500)
	in := strings.Repeat(long+"\n", 20) // 20 lines, ~10KB

	got := Truncate(in, TruncateOptions{MaxLines: 1000, MaxBytes: 2000})
	require.True(t, got.Truncated, "byte cap should bind even though the line cap does not")
	assert.LessOrEqual(t, got.KeptBytes, 2000)
	assert.Less(t, got.KeptLines, 20)
}

// Cutting on line boundaries means the retained text never ends mid-line.
func TestTruncate_CutsOnLineBoundaries(t *testing.T) {
	in := strings.Repeat("abcdefghij\n", 50) // 11 bytes per line
	got := Truncate(in, TruncateOptions{MaxBytes: 55})

	require.True(t, got.Truncated)
	for line := range strings.SplitSeq(got.Text, "\n") {
		assert.Equal(t, "abcdefghij", line, "every retained line should be whole")
	}
}

// A single line larger than the byte cap is the one case that must cut inside a
// line; it must still not split a multi-byte rune.
func TestTruncate_SingleOversizeLineCutsAtRuneBoundary(t *testing.T) {
	in := strings.Repeat("日", 1000) // 3 bytes per rune, one line, no newline

	head := Truncate(in, TruncateOptions{MaxBytes: 100})
	require.True(t, head.Truncated)
	assert.True(t, utf8.ValidString(head.Text), "head cut must stay on a rune boundary")
	assert.LessOrEqual(t, len(head.Text), 100)
	assert.Equal(t, 99, len(head.Text), "should back off to the largest whole-rune prefix")

	tail := Truncate(in, TruncateOptions{MaxBytes: 100, KeepTail: true})
	require.True(t, tail.Truncated)
	assert.True(t, utf8.ValidString(tail.Text), "tail cut must stay on a rune boundary")
	assert.True(t, strings.HasSuffix(in, tail.Text), "tail cut should retain the end of the line")
}

func TestTruncate_EmptyInput(t *testing.T) {
	got := Truncate("", TruncateOptions{})
	assert.False(t, got.Truncated)
	assert.Equal(t, "", got.String())
	assert.Equal(t, 0, got.TotalLines)
}

// The notice is the only signal the model gets that it is looking at a slice of
// the output rather than all of it, so it must be present and specific.
func TestTruncation_NoticeStatesWhatWasDropped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	got := Truncate(b.String(), TruncateOptions{MaxLines: 5, KeepTail: true})

	notice := got.Notice()
	assert.Contains(t, notice, "truncated")
	assert.Contains(t, notice, "last 5 of 50 lines")
	assert.Contains(t, got.String(), notice, "String should carry the notice through")
	assert.True(t, strings.HasPrefix(got.String(), got.Text))
}

func TestTruncation_NoticeEmptyWhenNothingDropped(t *testing.T) {
	assert.Equal(t, "", Truncate("short", TruncateOptions{}).Notice())
}
