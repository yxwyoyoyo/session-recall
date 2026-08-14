package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yxwyoyoyo/session-try/internal/session"
)

func TestKiroDiscoveryToSearchPipeline(t *testing.T) {
	home := t.TempDir()
	kiroHome := filepath.Join(home, ".kiro")
	sessionsDir := filepath.Join(kiroHome, "sessions", "cli")
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(binDir, "kiro-cli")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	id := "kiro-session-id"
	metadata, err := json.Marshal(map[string]string{
		"session_id": id,
		"cwd":        home,
		"updated_at": "2026-06-07T14:14:36Z",
		"title":      "Deploy the Kiro service",
	})
	if err != nil {
		t.Fatal(err)
	}
	journal := `{"version":"v1","kind":"Prompt","data":{"content":[{"kind":"text","data":"find the production deployment failure"}]}}`
	if err := os.WriteFile(filepath.Join(sessionsDir, id+".json"), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, id+".jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("KIRO_HOME", kiroHome)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--json", "--provider", "kiro", "deployment"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	var matches []session.Match
	if err := json.Unmarshal(stdout.Bytes(), &matches); err != nil {
		t.Fatalf("decode output: %v\noutput: %s", err, stdout.String())
	}
	if len(matches) != 1 || matches[0].Provider != "kiro" || matches[0].ID != id {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}
