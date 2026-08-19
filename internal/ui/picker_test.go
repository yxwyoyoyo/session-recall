package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

func TestTruncateDoesNotLeakANSIState(t *testing.T) {
	// A long title forces truncation of a fully styled wide-mode row. The
	// SGR state must be closed before the cut so dim/bold/color never
	// bleeds into the following row's marker and provider column.
	line := fmt.Sprintf("\x1b[1;33m→ \x1b[0m%s  \x1b[2m%s · \x1b[0m\x1b[94m%s\x1b[0m  \x1b[36m%s\x1b[0m",
		strings.Repeat("T", 120), "2h", "~/Tools/session-recall", "opencode")
	out := truncate(line, 60)
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Fatalf("truncated row does not close SGR state: %q", out)
	}
}

func TestTruncateCountsVisibleRunes(t *testing.T) {
	line := "\x1b[36mopencode\x1b[0m short"
	// width-1 visible runes survive, escape codes cost nothing.
	if got := truncate(line, 10); got != "\x1b[36mopencode\x1b[0m …\x1b[0m" {
		t.Fatalf("unexpected truncation: %q", got)
	}
}

func TestTruncateShortLineUntouched(t *testing.T) {
	line := "\x1b[36mclaude\x1b[0m short"
	if got := truncate(line, 40); got != line {
		t.Fatalf("short line was modified: %q", got)
	}
}

func TestTruncateExactWidthUntouched(t *testing.T) {
	if got := truncate("1234567890", 10); got != "1234567890" {
		t.Fatalf("line of exactly width was modified: %q", got)
	}
	if got := truncate("\x1b[2m1234567890\x1b[0m", 10); got != "\x1b[2m1234567890\x1b[0m" {
		t.Fatalf("styled line of exactly width was modified: %q", got)
	}
	if got := truncate("12345678901", 10); got != "123456789…" {
		t.Fatalf("line over width not truncated correctly: %q", got)
	}
}

func TestTruncateHandlesNonSGRSequences(t *testing.T) {
	line := "\x1b[2K123456789012"
	if got := truncate(line, 10); got != "\x1b[2K123456789…\x1b[0m" {
		t.Fatalf("non-SGR sequence upset truncation: %q", got)
	}
}

func TestTruncateSkipsOSCPayloadWidth(t *testing.T) {
	// OSC 8 hyperlinks and OSC 0 title changes carry payloads; only the
	// text after the ST terminator counts toward the visible width.
	line := "\x1b]8;;https://example.com\x1b\\visible content"
	if got := truncate(line, 9); got != "\x1b]8;;https://example.com\x1b\\visible …\x1b[0m" {
		t.Fatalf("OSC payload counted toward width: %q", got)
	}
	if got := truncate(line, 100); got != line {
		t.Fatalf("short OSC line was modified: %q", got)
	}
}

func TestTruncateSkipsBELTerminatedOSC(t *testing.T) {
	line := "\x1b]0;title\x07abcdefghijklmnop"
	if got := truncate(line, 6); got != "\x1b]0;title\x07abcde…\x1b[0m" {
		t.Fatalf("BEL-terminated OSC mishandled: %q", got)
	}
}

func TestTruncateHandlesTwoByteSequences(t *testing.T) {
	line := "\x1b7abcdef"
	if got := truncate(line, 5); got != "\x1b7abcd…\x1b[0m" {
		t.Fatalf("two-byte sequence upset truncation: %q", got)
	}
}

func TestTruncateHandlesNestedESCAndSelectors(t *testing.T) {
	line := "\x1b\x1b[31m\x1b%G\x1b(Blongtext"
	if got := truncate(line, 5); got != "\x1b\x1b[31m\x1b%G\x1b(Blong…\x1b[0m" {
		t.Fatalf("nested ESC or selector bytes upset truncation: %q", got)
	}
	if got := truncate(line, 100); got != line {
		t.Fatalf("short nested-ESC line was modified: %q", got)
	}
}

func TestVisibleRunes(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"hello", 5},
		{"\x1b[36mx\x1b[0m", 1},
		{"\x1b[2K\x1b[94mabc\x1b[0m", 3},
		{"\x1b]8;;https://example.com\x1b\\hi \x1b]0;t\x07", 3},
		{"→ ", 2},
	}
	for _, c := range cases {
		if got := visibleRunes(c.text); got != c.want {
			t.Fatalf("visibleRunes(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}

func TestViewSnippetTopN(t *testing.T) {
	results := make([]session.Match, 6)
	for i := range results {
		results[i] = session.Match{
			Session: session.Session{
				Provider:  "opencode",
				ID:        fmt.Sprintf("id-%d", i),
				Title:     fmt.Sprintf("Title %d", i),
				Directory: "/tmp",
				UpdatedAt: time.Now(),
			},
			Snippet: fmt.Sprintf("unique-snippet-%d", i),
		}
	}
	picker := &Picker{input: textinput.New(), results: results, width: 120, height: 60, cursor: 5}
	view := picker.View()
	for i := 0; i < 3; i++ {
		if !strings.Contains(view, fmt.Sprintf("unique-snippet-%d", i)) {
			t.Fatalf("top snippet %d missing in view: %q", i, view)
		}
	}
	if !strings.Contains(view, "unique-snippet-5") {
		t.Fatalf("cursor row snippet missing in view: %q", view)
	}
	if got := strings.Count(view, "unique-snippet-"); got != 4 {
		t.Fatalf("expected 4 snippets (top 3 + cursor), got %d", got)
	}
}

func TestViewRowLayout(t *testing.T) {
	now := time.Now()
	results := []session.Match{
		{
			Session: session.Session{
				Provider:  "opencode",
				Title:     strings.Repeat("T", 120),
				Directory: "/workspace/session-recall",
				UpdatedAt: now,
			},
		},
		{
			Session: session.Session{
				Provider:  "claude",
				Title:     "Short Title",
				Directory: "/workspace/claude",
				UpdatedAt: now,
			},
		},
	}
	wide := &Picker{input: textinput.New(), results: results, width: 120, height: 60}
	wview := wide.View()
	if strings.Contains(wview, strings.Repeat("T", 120)) {
		t.Fatalf("wide mode left long title untruncated")
	}
	for _, line := range strings.Split(wview, "\n") {
		if strings.HasPrefix(line, "  Short Title") && !strings.HasSuffix(line, "\x1b[36mclaude\x1b[0m") {
			t.Fatalf("wide mode row not right-aligned with provider at end: %q", line)
		}
	}

	narrow := &Picker{input: textinput.New(), results: results, width: 60, height: 60}
	nview := narrow.View()
	if !strings.Contains(nview, "Short Title") {
		t.Fatalf("narrow mode lost short title: %q", nview)
	}
	if !strings.Contains(nview, "  \x1b[2m") {
		t.Fatalf("narrow mode missing indented meta line: %q", nview)
	}
	if !strings.Contains(nview, "/workspace/session-recall") {
		t.Fatalf("narrow mode lost directory: %q", nview)
	}
	if !strings.Contains(nview, "\x1b[36mopencode\x1b[0m") {
		t.Fatalf("narrow mode lost provider: %q", nview)
	}
}
