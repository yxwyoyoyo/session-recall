package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

func TestResolveIndexPathMigratesLegacyDirectory(t *testing.T) {
	cache := t.TempDir()
	legacyDir := filepath.Join(cache, legacyAppName)
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyIndex := filepath.Join(legacyDir, "index.db")
	if err := os.WriteFile(legacyIndex, []byte("existing index"), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := resolveIndexPath(cache)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cache, appName, "index.db")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "existing index" {
		t.Fatalf("migrated index = %q", contents)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory still exists: %v", err)
	}
}

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
	t.Chdir(home)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-j", "-p", "kiro", "-c", "-n", "1", "deployment"}, &stdout, &stderr); err != nil {
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

func TestPiDiscoveryToSearchPipeline(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, ".pi", "agent")
	sessionsDir := filepath.Join(agentDir, "sessions", "--pi-project--")
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(binDir, "pi")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	id := "pi-session-id"
	journal := strings.Join([]string{
		`{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-08-17T10:00:00.000Z","cwd":"` + home + `"}`,
		`{"type":"message","id":"a1b2c3d4","parentId":null,"timestamp":"2026-08-17T10:00:01.000Z","message":{"role":"user","content":"find the Pi session discovery failure","timestamp":1786960801000}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(sessionsDir, "pi-session.jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(home)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-j", "-p", "pi", "-c", "-n", "1", "discovery"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	var matches []session.Match
	if err := json.Unmarshal(stdout.Bytes(), &matches); err != nil {
		t.Fatalf("decode output: %v\noutput: %s", err, stdout.String())
	}
	if len(matches) != 1 || matches[0].Provider != "pi" || matches[0].ID != id {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}
