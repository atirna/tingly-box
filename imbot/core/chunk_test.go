package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// runeCount is a local helper to keep the test self-contained.
func runeCount(s string) int { return utf8.RuneCountInString(s) }

func TestChunkTextForPlatform_NoSplit(t *testing.T) {
	// A single chunk when under the limit.
	got := ChunkTextForPlatform(PlatformDiscord, "short")
	if len(got) != 1 || got[0] != "short" {
		t.Fatalf("got %v", got)
	}
}

func TestChunkTextForPlatform_NeverSplitsRune(t *testing.T) {
	// Discord limit is 2000 (characters). Build a string of emoji that is well
	// over the limit in runes (and even more in bytes), then assert every chunk
	// is valid UTF-8 and the concatenation round-trips exactly. The previous
	// byte-based chunker could slice inside a multi-byte rune here.
	const marker = "😀" // 4 bytes, 1 rune
	big := strings.Repeat(marker, 5000) // 5000 runes, 20000 bytes

	chunks := ChunkTextForPlatform(PlatformDiscord, big)

	var rebuilt strings.Builder
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Fatalf("chunk %d is not valid UTF-8", i)
		}
		if c == "" {
			t.Fatalf("chunk %d is empty", i)
		}
		if runeCount(c) > 2000 {
			t.Fatalf("chunk %d has %d runes, exceeds limit 2000", i, runeCount(c))
		}
		rebuilt.WriteString(c)
	}
	if rebuilt.String() != big {
		t.Fatal("concatenated chunks do not round-trip the original")
	}
}

func TestChunkTextForPlatform_CountsByRuneNotByte(t *testing.T) {
	// 2000 emoji runes = 2000 characters but 8000 bytes. This must NOT be chunked
	// (it is exactly at the Discord limit by character count). A byte-based
	// counter would have split it.
	exact := strings.Repeat("😀", 2000)
	got := ChunkTextForPlatform(PlatformDiscord, exact)
	if len(got) != 1 {
		t.Fatalf("expected single chunk for rune-exact text, got %d chunks", len(got))
	}
}

func TestChunkTextForPlatform_PrefersNewlineBreak(t *testing.T) {
	// Two paragraphs separated by a newline near the limit; the chunker should
	// break at the newline rather than mid-word.
	lineA := strings.Repeat("a", 1900)
	lineB := strings.Repeat("b", 1900)
	text := lineA + "\n" + lineB

	chunks := ChunkTextForPlatform(PlatformDiscord, text)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d (%v)", len(chunks), chunks)
	}
	if chunks[0] != lineA+"\n" {
		t.Fatalf("first chunk should end at newline, got trailing %q", chunks[0][len(lineA):])
	}
	if chunks[1] != lineB {
		t.Fatalf("second chunk mismatch")
	}
}

func TestChunkTextForPlatform_KeepsCodeFenceWhole(t *testing.T) {
	// A fenced block whose end sits just past the limit should not be split open.
	inside := strings.Repeat("x", 1990)
	text := "```\n" + inside + "\n```"

	chunks := ChunkTextForPlatform(PlatformDiscord, text)
	// Each chunk must have balanced fences (even count of triple-backtick).
	for i, c := range chunks {
		fences := strings.Count(c, "```")
		if fences%2 != 0 {
			t.Fatalf("chunk %d splits a code fence open (fence count=%d)", i, fences)
		}
	}
}
