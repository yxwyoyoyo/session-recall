package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexFileUsesMetadataTitleAndUserContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := strings.Join([]string{
		`{"timestamp":"2026-01-01T10:00:00Z","type":"session_meta","payload":{"id":"uuid-1","cwd":"/repo","timestamp":"2026-01-01T10:00:00Z"}}`,
		`{"timestamp":"2026-01-01T10:00:30Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"injected workspace instructions"}]}}`,
		`{"timestamp":"2026-01-01T10:01:00Z","type":"event_msg","payload":{"type":"user_message","message":"find the missing session"}}`,
		`{"timestamp":"2026-01-01T10:02:00Z","type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"do not index this"}]}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	item, _, err := parseCodexFile(path, 7, map[string]codexTitle{"uuid-1": {ID: "uuid-1", ThreadName: "Readable title"}})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "uuid-1" || item.Directory != "/repo" || item.Title != "Readable title" {
		t.Fatalf("unexpected session: %#v", item)
	}
	if item.Content != "find the missing session\n" {
		t.Fatalf("unexpected content: %q", item.Content)
	}
}

func TestParseCodexFileRejectsChangedUserMessageShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := strings.Join([]string{
		`{"timestamp":"2026-01-01T10:00:00Z","type":"session_meta","payload":{"id":"uuid-1","cwd":"/repo"}}`,
		`{"timestamp":"2026-01-01T10:01:00Z","type":"event_msg","payload":{"type":"user_message","message":{"text":"new shape"}}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, skipped, err := parseCodexFile(path, 7, nil)
	if err == nil || skipped != 1 || !strings.Contains(err.Error(), "session record") {
		t.Fatalf("expected incompatible shape, skipped=%d err=%v", skipped, err)
	}
}

func TestParseCodexFileRejectsMissingUserMessageField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := strings.Join([]string{
		`{"timestamp":"2026-01-01T10:00:00Z","type":"session_meta","payload":{"id":"uuid-1","cwd":"/repo"}}`,
		`{"timestamp":"2026-01-01T10:01:00Z","type":"event_msg","payload":{"type":"user_message","content":"renamed field"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, skipped, err := parseCodexFile(path, 7, nil)
	if err == nil || skipped != 1 || !strings.Contains(err.Error(), "session record") {
		t.Fatalf("expected missing message field to be incompatible, skipped=%d err=%v", skipped, err)
	}
}

func TestParseCodexFileRejectsMalformedRecordAfterValidContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := strings.Join([]string{
		`{"timestamp":"2026-01-01T10:00:00Z","type":"session_meta","payload":{"id":"uuid-1","cwd":"/repo"}}`,
		`{"timestamp":"2026-01-01T10:01:00Z","type":"event_msg","payload":{"type":"user_message","message":"new prompt"}}`,
		`{"broken"`,
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, skipped, err := parseCodexFile(path, 7, nil)
	if err == nil || skipped != 1 || !strings.Contains(err.Error(), "session record") {
		t.Fatalf("expected malformed record to reject the source, skipped=%d err=%v", skipped, err)
	}
}
