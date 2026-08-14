package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkParseCodex1000Messages(b *testing.B) {
	path := filepath.Join(b.TempDir(), "codex.jsonl")
	var data strings.Builder
	data.WriteString(`{"timestamp":"2026-01-01T10:00:00Z","type":"session_meta","payload":{"id":"uuid-1","cwd":"/workspace/project","timestamp":"2026-01-01T10:00:00Z"}}`)
	data.WriteByte('\n')
	for i := range 1_000 {
		fmt.Fprintf(&data, "{\"timestamp\":\"2026-01-01T10:01:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"investigate pane lifecycle prompt %d\"}}\n", i)
	}
	if err := os.WriteFile(path, []byte(data.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	titles := map[string]codexTitle{"uuid-1": {ID: "uuid-1", ThreadName: "Pane lifecycle"}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		item, err := parseCodexFile(path, 1, titles)
		if err != nil {
			b.Fatal(err)
		}
		if item.ID != "uuid-1" {
			b.Fatalf("unexpected session: %#v", item)
		}
	}
}

func BenchmarkParseKiro1000Prompts(b *testing.B) {
	dir := b.TempDir()
	metaPath := filepath.Join(dir, "kiro.json")
	journalPath := filepath.Join(dir, "kiro.jsonl")
	metadata := `{"session_id":"kiro","cwd":"/workspace/project","updated_at":"2026-01-01T10:00:00Z","title":"Pane lifecycle"}`
	if err := os.WriteFile(metaPath, []byte(metadata), 0o600); err != nil {
		b.Fatal(err)
	}
	var data strings.Builder
	for i := range 1_000 {
		fmt.Fprintf(&data, "{\"version\":\"v1\",\"kind\":\"Prompt\",\"data\":{\"content\":[{\"kind\":\"text\",\"data\":\"investigate pane lifecycle prompt %d\"}]}}\n", i)
	}
	if err := os.WriteFile(journalPath, []byte(data.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		item, err := parseKiroSession(metaPath, journalPath, "kiro")
		if err != nil {
			b.Fatal(err)
		}
		if item.ID != "kiro" {
			b.Fatalf("unexpected session: %#v", item)
		}
	}
}
