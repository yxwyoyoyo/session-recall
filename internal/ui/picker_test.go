package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

func TestTruncateDoesNotLeakANSIState(t *testing.T) {
	// A long title forces truncation of a fully styled wide-mode row. The
	// SGR state must be closed before the cut so dim/bold/color never
	// bleeds into the following row's marker and provider column.
	line := fmt.Sprintf("\x1b[1;33m→  \x1b[0m%s \x1b[2m2h\x1b[0m                                         \x1b[2m%s\x1b[0m \x1b[36mopencode \x1b[0m",
		strings.Repeat("T", 120), "~/Tools/session-recall")
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
	if got := truncate("12345678901", 10); got != "123456789…\x1b[0m" {
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

func TestHighlight(t *testing.T) {
	const open, close = "\x1b[1;33m", "\x1b[0m"
	cases := []struct {
		text, query, want string
	}{
		{"hello world", "world", "hello " + open + "world" + close},
		{"Hello World", "hello", open + "Hello" + close + " World"},
		{"调试 picker 泄漏", "picker", "调试 " + open + "picker" + close + " 泄漏"},
		{"调试 picker 泄漏", "调试", open + "调试" + close + " picker 泄漏"},
		{"abab", "ab", open + "ab" + close + open + "ab" + close},
		{"no match", "zz", "no match"},
		{"plain", "", "plain"},
		{"调试调试", "调试", open + "调试" + close + open + "调试" + close},
	}
	for _, c := range cases {
		got := highlight(c.text, strings.Fields(strings.ToLower(c.query)), open, close)
		if got != c.want {
			t.Fatalf("highlight(%q, %q) = %q, want %q", c.text, c.query, got, c.want)
		}
	}
}

func TestHighlightEarliestTokenFirst(t *testing.T) {
	const open, close = "\x1b[1;33m", "\x1b[0m"
	got := highlight("a X b x c", []string{"x", "X"}, open, close)
	want := "a " + open + "X" + close + " b " + open + "x" + close + " c"
	if got != want {
		t.Fatalf("highlight = %q, want %q", got, want)
	}
}

func TestTruncateRespectsCellWidth(t *testing.T) {
	// Each CJK rune is 2 cells wide; the cut must not split them.
	if got := truncate("调试代码测试内容", 6); got != "调试…\x1b[0m" {
		t.Fatalf("wide-char truncation wrong: %q", got)
	}
	if got := truncate("调试代码测试内容", 8); got != "调试代…\x1b[0m" {
		t.Fatalf("wide-char truncation wrong: %q", got)
	}
	if got := truncate("ab调试cd", 6); got != "ab调…\x1b[0m" {
		t.Fatalf("mixed-width truncation wrong: %q", got)
	}
	if got := ansi.StringWidth(truncate("调试代码测试内容", 10)); got > 10 {
		t.Fatalf("truncated line wider than budget: %d", got)
	}
}

func TestViewCJKRightAlign(t *testing.T) {
	now := time.Now()
	results := []session.Match{{
		Session: session.Session{
			Provider:  "opencode",
			Title:     "调试 picker ANSI 泄漏与右对齐",
			Directory: "/workspace/session-recall",
			UpdatedAt: now,
		},
	}}
	picker := &Picker{input: textinput.New(), results: results, width: 100, height: 20}
	for _, line := range strings.Split(picker.View(), "\n") {
		if !strings.Contains(line, "\x1b[36mopencode") {
			continue
		}
		if got := ansi.StringWidth(line); got > 100 {
			t.Fatalf("right-aligned row wider than terminal (%d cells): %q", got, line)
		}
	}
}

func TestHighlightFoldsDiacritics(t *testing.T) {
	const open, close = "\x1b[1;33m", "\x1b[0m"
	got := highlight("Café ordering", []string{"cafe"}, open, close)
	want := open + "Café" + close + " ordering"
	if got != want {
		t.Fatalf("highlight = %q, want %q", got, want)
	}
}

func TestHighlightSurvivesLowercaseByteShift(t *testing.T) {
	const open, close = "\x1b[1;33m", "\x1b[0m"
	// ẞ lowercases to ß (3 bytes -> 2), shifting later offsets; the offset
	// map must come from builder positions or the wrap goes stale.
	if got := highlight("ẞest", []string{"est"}, open, close); got != "ẞ"+open+"est"+close {
		t.Fatalf("highlight = %q", got)
	}
	if got := highlight("menu ẞaß", []string{"a"}, open, close); got != "menu ẞ"+open+"a"+close+"ß" {
		t.Fatalf("highlight = %q", got)
	}
	// A lone jamo matches only a prefix of Hangul's canonical decomposition:
	// the mapped span is empty, so no empty styled pair may be emitted.
	if got := highlight("한", []string{"ᄒ"}, open, close); got != "한" {
		t.Fatalf("highlight = %q", got)
	}
}

func TestViewHighlightQueryTerms(t *testing.T) {
	now := time.Now()
	input := textinput.New()
	input.SetValue("picker")
	results := []session.Match{{
		Session: session.Session{
			Provider:  "opencode",
			Title:     "Fix the picker truncation bug",
			Directory: "/workspace/session-recall",
			UpdatedAt: now,
		},
		Snippet: "The picker rows handle ANSI leaks.",
	}}
	picker := &Picker{input: input, results: results, width: 100, height: 20, cursor: 0}
	view := picker.View()
	if !strings.Contains(view, "\x1b[1;33mpicker\x1b[0m") {
		t.Fatalf("query term not highlighted in title: %q", view)
	}
	if !strings.Contains(view, "\x1b[0m\x1b[1;33mpicker\x1b[0m\x1b[38;5;249m") {
		t.Fatalf("query term not highlighted in snippet: %q", view)
	}
}

func TestViewSnippetOnlyForCursor(t *testing.T) {
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
	if !strings.Contains(view, "unique-snippet-5") {
		t.Fatalf("cursor row snippet missing in view: %q", view)
	}
	for i := 0; i < 5; i++ {
		if strings.Contains(view, fmt.Sprintf("unique-snippet-%d", i)) {
			t.Fatalf("snippet %d shown for a non-cursor row: %q", i, view)
		}
	}
	if !strings.Contains(view, "opencode \x1b[0m\n    \x1b[38;5;249munique-snippet-5\x1b[0m\n\n\x1b[2m↑↓") {
		t.Fatalf("cursor content not indented under its row: %q", view)
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
	var slotStart = -1
	for _, line := range strings.Split(wview, "\n") {
		if !strings.Contains(line, "\x1b[36m") {
			continue
		}
		if got := ansi.StringWidth(line); got != 120 {
			t.Fatalf("wide mode row width %d, want 120: %q", got, line)
		}
		idx := strings.Index(line, "\x1b[36m")
		start := ansi.StringWidth(line[:idx])
		if slotStart == -1 {
			slotStart = start
		}
		if start != slotStart {
			t.Fatalf("provider first char not aligned: %q", line)
		}
	}
	if slotStart != 111 {
		t.Fatalf("provider column starts at %d, want 111 (width-9)", slotStart)
	}

	compact := &Picker{input: textinput.New(), results: results, width: 60, height: 60}
	cview := compact.View()
	if !strings.Contains(cview, "Short Title") {
		t.Fatalf("compact width lost short title: %q", cview)
	}
	if !strings.Contains(cview, "/workspace/claude") {
		t.Fatalf("compact width lost short row directory: %q", cview)
	}
	if !strings.Contains(cview, "T…") {
		t.Fatalf("compact width should truncate the long title: %q", cview)
	}
	for _, line := range strings.Split(cview, "\n") {
		if strings.Contains(line, "\x1b[36m") && ansi.StringWidth(line) > 60 {
			t.Fatalf("compact width row wider than terminal (%d cells): %q", ansi.StringWidth(line), line)
		}
	}
}
