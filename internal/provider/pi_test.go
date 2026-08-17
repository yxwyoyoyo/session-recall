package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

func TestPiDiscoverReadsNamedSessionAndUserText(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "--workspace-pi-project--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := strings.Join([]string{
		`{"type":"session","version":3,"id":"13d47718-729e-43d4-b197-36fc890767f5","timestamp":"2026-08-17T10:00:00.000Z","cwd":"/workspace/pi-project"}`,
		`{"type":"message","id":"a1b2c3d4","parentId":null,"timestamp":"2026-08-17T10:00:01.000Z","message":{"role":"user","content":"find the session indexing bug","timestamp":1786960801000}}`,
		`{"type":"message","id":"b2c3d4e5","parentId":"a1b2c3d4","timestamp":"2026-08-17T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"assistant text is not indexed"}],"timestamp":1786960802000}}`,
		`{"type":"message","id":"c3d4e5f6","parentId":"b2c3d4e5","timestamp":"2026-08-17T10:00:03.000Z","message":{"role":"user","content":[{"type":"text","text":"include array text too"},{"type":"image","data":"ignored"}],"timestamp":1786960803000}}`,
		`{"type":"session_info","id":"d4e5f6a7","parentId":"c3d4e5f6","timestamp":"2026-08-17T10:00:04.000Z","name":"Repair Pi discovery"}`,
	}, "\n")
	path := filepath.Join(dir, "2026-08-17T10-00-00-000Z_13d47718.jsonl")
	if err := os.WriteFile(path, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &Pi{SessionDir: filepath.Dir(dir), Executable: "pi"}
	discovery, err := p.Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	items := discovery.Sessions
	if len(items) != 1 {
		t.Fatalf("got %d sessions", len(items))
	}
	item := items[0]
	if item.ID != "13d47718-729e-43d4-b197-36fc890767f5" || item.Directory != "/workspace/pi-project" || item.Title != "Repair Pi discovery" {
		t.Fatalf("unexpected session: %#v", item)
	}
	if item.Content != "find the session indexing bug\ninclude array text too\n" {
		t.Fatalf("unexpected content: %q", item.Content)
	}
	known := map[string]int64{item.Source: item.Stamp}
	unchanged, err := p.Discover(context.Background(), known)
	if err != nil || len(unchanged.Sessions) != 0 || unchanged.Unchanged != 1 {
		t.Fatalf("expected unchanged source to be skipped: %#v, %v", unchanged, err)
	}
}

func TestNewPiRespectsEnvironmentAndSettings(t *testing.T) {
	agentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(`{"sessionDir":"custom-sessions"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	p := NewPi("/ignored")
	if p.AgentDir != agentDir || p.SessionDir != filepath.Join(agentDir, "custom-sessions") {
		t.Fatalf("unexpected directories: agent=%q sessions=%q", p.AgentDir, p.SessionDir)
	}

	override := filepath.Join(t.TempDir(), "sessions")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", override)
	p = NewPi("/ignored")
	if p.SessionDir != override {
		t.Fatalf("got session dir %q, want %q", p.SessionDir, override)
	}

	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, "agent"))
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "~/pi-sessions")
	p = NewPi(home)
	if p.SessionDir != filepath.Join(home, "pi-sessions") {
		t.Fatalf("got expanded session dir %q", p.SessionDir)
	}
}

func TestPiResumeCommandUsesExactSessionFile(t *testing.T) {
	p := &Pi{Executable: "pi"}
	cmd, err := p.ResumeCommand(session.Session{ID: "13d47718", Source: "/sessions/pi.jsonl", Directory: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pi", "--session", "/sessions/pi.jsonl"}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") || cmd.Dir != "/repo" {
		t.Fatalf("unexpected command: %#v, dir=%q", cmd.Args, cmd.Dir)
	}
}

func TestPiIsAvailableBeforeFirstSession(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &Pi{Executable: executable, SessionDir: filepath.Join(t.TempDir(), "missing")}
	if !p.Available() {
		t.Fatal("installed Pi should be available before its first session")
	}
}

func TestPiDiscoverReportsUnknownSchemaWithoutReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changed.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"new_session_shape","uuid":"pi-new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Pi{SessionDir: dir, Executable: "pi"}
	discovery, err := p.Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Sessions) != 0 || len(discovery.Failures) != 1 {
		t.Fatalf("unexpected discovery: %#v", discovery)
	}
	if discovery.Failures[0].Source != path || !strings.Contains(discovery.Failures[0].Err.Error(), "session header") {
		t.Fatalf("unexpected failure: %#v", discovery.Failures[0])
	}
}

func TestPiDiscoverRejectsChangedUserContentShape(t *testing.T) {
	dir := t.TempDir()
	journal := strings.Join([]string{
		`{"type":"session","version":4,"id":"pi-new","timestamp":"2026-08-17T10:00:00Z","cwd":"/repo"}`,
		`{"type":"message","timestamp":"2026-08-17T10:00:01Z","message":{"role":"user","content":{"text":"new object shape"}}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "changed.jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Pi{SessionDir: dir, Executable: "pi"}
	discovery, err := p.Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Sessions) != 0 || len(discovery.Failures) != 1 || discovery.SkippedRecords != 1 {
		t.Fatalf("unexpected discovery: %#v", discovery)
	}
	if !strings.Contains(discovery.Failures[0].Err.Error(), "content shape") {
		t.Fatalf("unexpected failure: %v", discovery.Failures[0].Err)
	}
}

func TestPiDiscoverRejectsUnknownArrayContentPart(t *testing.T) {
	dir := t.TempDir()
	journal := strings.Join([]string{
		`{"type":"session","version":4,"id":"pi-new","timestamp":"2026-08-17T10:00:00Z","cwd":"/repo"}`,
		`{"type":"message","timestamp":"2026-08-17T10:00:01Z","message":{"role":"user","content":[{"type":"input_text","text":"renamed part"}]}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "changed.jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err := (&Pi{SessionDir: dir, Executable: "pi"}).Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Sessions) != 0 || len(discovery.Failures) != 1 || discovery.SkippedRecords != 1 {
		t.Fatalf("unknown array part must not be applied: %#v", discovery)
	}
	if !strings.Contains(discovery.Failures[0].Err.Error(), "content shape") {
		t.Fatalf("unexpected failure: %v", discovery.Failures[0].Err)
	}
}
