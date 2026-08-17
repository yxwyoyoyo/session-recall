package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeDiscoverAggregatesUserPrompts(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	history := "" +
		`{"display":"fix the pane lifecycle","project":"/repo","sessionId":"abc","timestamp":1000}` + "\n" +
		`{"display":"/status","project":"/repo","sessionId":"abc","timestamp":2000}` + "\n" +
		`{"display":"persist after reattach","project":"/repo","sessionId":"abc","timestamp":3000}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewClaude(home)
	discovery, err := p.Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	items := discovery.Sessions
	if len(items) != 1 {
		t.Fatalf("got %d sessions", len(items))
	}
	if items[0].Title != "fix the pane lifecycle" || items[0].Directory != "/repo" {
		t.Fatalf("unexpected session: %#v", items[0])
	}
	if items[0].Content != "fix the pane lifecycle\npersist after reattach\n" {
		t.Fatalf("unexpected content: %q", items[0].Content)
	}
	known := map[string]int64{items[0].Source: items[0].Stamp}
	unchanged, err := p.Discover(context.Background(), known)
	if err != nil || len(unchanged.Sessions) != 0 || unchanged.Unchanged != 1 {
		t.Fatalf("expected unchanged source to be skipped: %#v, %v", unchanged, err)
	}
}

func TestClaudeDiscoverRejectsPartialSharedHistory(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	history := strings.Join([]string{
		`{"display":"keep this session","project":"/repo","sessionId":"old","timestamp":1000}`,
		`{"prompt":"new history shape","cwd":"/repo","session_id":"new","timestamp":2000}`,
	}, "\n")
	path := filepath.Join(dir, "history.jsonl")
	if err := os.WriteFile(path, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err := NewClaude(home).Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Sessions) != 0 || len(discovery.Failures) != 1 || discovery.SkippedRecords != 1 {
		t.Fatalf("partial shared history must not be applied: %#v", discovery)
	}
	if discovery.Failures[0].Source != path || !strings.Contains(discovery.Failures[0].Err.Error(), "unrecognized") {
		t.Fatalf("unexpected failure: %#v", discovery.Failures[0])
	}
}
