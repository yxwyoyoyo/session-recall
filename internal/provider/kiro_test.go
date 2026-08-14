package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yxwyoyoyo/session-try/internal/session"
)

func TestKiroDiscoverReadsMetadataAndPromptText(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "0a5376f2-7e2f-4981-bcbc-67195586604a"
	metadata := `{
		"session_id":"` + id + `",
		"cwd":"/workspace/kiro-project",
		"created_at":"2026-06-07T14:14:27.290365Z",
		"updated_at":"2026-06-07T14:14:36.404077Z",
		"title":"Investigate Kiro session discovery",
		"session_state":{"version":"v1"}
	}`
	journal := strings.Join([]string{
		`{"version":"v1","kind":"Prompt","data":{"content":[{"kind":"text","data":"find the deployment session"}],"message_id":"one","meta":{"timestamp":1780841667}}}`,
		`{"version":"v1","kind":"AssistantMessage","data":{"content":[{"kind":"text","data":"assistant text is not indexed"}],"message_id":"two"}}`,
		`{"version":"v1","kind":"ToolResults","data":{"content":[{"kind":"toolResult","data":{"status":"success"}}],"message_id":"three"}}`,
	}, "\n")
	metaPath := filepath.Join(dir, id+".json")
	journalPath := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(metaPath, []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &Kiro{Home: home, Executable: "kiro-cli"}
	items, err := p.Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d sessions", len(items))
	}
	item := items[0]
	if item.ID != id || item.Directory != "/workspace/kiro-project" || item.Title != "Investigate Kiro session discovery" {
		t.Fatalf("unexpected session: %#v", item)
	}
	if item.Content != "find the deployment session\n" {
		t.Fatalf("unexpected content: %q", item.Content)
	}
	known := map[string]int64{item.Source: item.Stamp}
	unchanged, err := p.Discover(context.Background(), known)
	if err != nil || len(unchanged) != 0 {
		t.Fatalf("expected unchanged source to be skipped: %#v, %v", unchanged, err)
	}
}

func TestKiroDiscoverToleratesJournalWithoutMetadata(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := `{"version":"v1","kind":"Prompt","data":{"content":[{"kind":"text","data":"recover this conversation"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "journal-only.jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Kiro{Home: home, Executable: "kiro-cli"}
	items, err := p.Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "journal-only" || items[0].Title != "recover this conversation" {
		t.Fatalf("unexpected sessions: %#v", items)
	}
}

func TestKiroResumeCommandUsesExplicitChatSubcommand(t *testing.T) {
	p := &Kiro{Executable: "kiro-cli"}
	cmd, err := p.ResumeCommand(sessionFixture("kiro", "abc-123", "/repo"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kiro-cli", "chat", "--resume-id", "abc-123"}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") || cmd.Dir != "/repo" {
		t.Fatalf("unexpected command: %#v, dir=%q", cmd.Args, cmd.Dir)
	}
}

func TestNewKiroRespectsKiroHome(t *testing.T) {
	configured := filepath.Join(t.TempDir(), "custom-kiro")
	t.Setenv("KIRO_HOME", configured)
	p := NewKiro("/ignored")
	if p.Home != configured {
		t.Fatalf("got home %q, want %q", p.Home, configured)
	}
}

func sessionFixture(provider, id, directory string) session.Session {
	return session.Session{Provider: provider, ID: id, Directory: directory}
}
