package ui

import (
	"fmt"
	"strings"
	"testing"
)

func TestTruncateDoesNotLeakANSIState(t *testing.T) {
	// A long directory forces truncation of a fully formatted row. The
	// SGR state must be closed before the cut so dim/bold never bleeds into
	// the following row's marker and provider column.
	line := fmt.Sprintf("\x1b[36m%-9s\x1b[0m %s  \x1b[2m%s · %s\x1b[0m",
		"opencode", "Title", strings.Repeat("a", 120), "2h")
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
