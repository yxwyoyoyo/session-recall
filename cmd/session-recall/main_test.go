package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yxwyoyoyo/session-recall/internal/config"
	"github.com/yxwyoyoyo/session-recall/internal/index"
	"github.com/yxwyoyoyo/session-recall/internal/provider"
	"github.com/yxwyoyoyo/session-recall/internal/session"
)

type doctorProvider struct {
	revision int
}

func (doctorProvider) Name() string          { return "test-provider" }
func (p doctorProvider) ParserRevision() int { return p.revision }
func (doctorProvider) Available() bool       { return true }
func (doctorProvider) Discover(context.Context, map[string]int64) (provider.Discovery, error) {
	return provider.Discovery{}, nil
}
func (doctorProvider) ResumeCommand(session.Session) (*exec.Cmd, error) { return nil, nil }

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

func TestIndexPathForUsesRcDatabaseOverride(t *testing.T) {
	cache := t.TempDir()
	home := t.TempDir()
	path, err := indexPathFor(config.Config{Database: "~/dedicated/index.db"}, cache, home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "dedicated", "index.db")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("database mode = %o, want 600", got)
		}
	}
}

func TestIndexPathForTightensExistingDatabasePermissions(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "index.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := indexPathFor(config.Config{Database: path}, t.TempDir(), home); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("database mode = %o, want 600", got)
		}
	}
}

func TestHelpAndVersionIgnoreMalformedRc(t *testing.T) {
	rc := filepath.Join(t.TempDir(), "rc")
	if err := os.WriteFile(rc, []byte("database =\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SESSION_RECALL_RC", rc)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"--version"}, want: version + "\n"},
		{args: []string{"--help"}, want: "Usage:"},
	} {
		var stdout, stderr bytes.Buffer
		if err := run(tc.args, &stdout, &stderr); err != nil {
			t.Fatalf("run(%v): %v", tc.args, err)
		}
		if !strings.Contains(stdout.String(), tc.want) {
			t.Fatalf("run(%v) output = %q, want %q", tc.args, stdout.String(), tc.want)
		}
	}
}

func TestRunUsesRcDatabaseOverride(t *testing.T) {
	home := t.TempDir()
	dedicated := filepath.Join(home, "private", "index.db")
	rcDir := filepath.Join(home, ".config", config.AppName)
	if err := os.MkdirAll(rcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(rcDir, "rc")
	if err := os.WriteFile(rc, []byte("database = \""+dedicated+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SESSION_RECALL_RC", "")
	t.Chdir(home)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "config\t"+rc+"\n") {
		t.Fatalf("doctor output missing rc path:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "index\t"+dedicated+"\n") {
		t.Fatalf("doctor output missing dedicated index:\n%s", stdout.String())
	}
	if _, err := os.Stat(dedicated); err != nil {
		t.Fatalf("dedicated database not created: %v", err)
	}
}

func TestRunIgnoresMissingRcFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SESSION_RECALL_RC", filepath.Join(home, "nonexistent", "rc"))
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(home)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), "config\t") {
		t.Fatalf("missing rc file should not print config line:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "index\t"+filepath.Join(cache, config.AppName, "index.db")) {
		t.Fatalf("default index path not used:\n%s", stdout.String())
	}
}

func TestDoctorReportsDegradedProviderDiagnostics(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	store, err := index.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.ApplyRefresh(context.Background(), "test-provider", 3, nil, index.RefreshStats{
		Scanned: 4, Changed: 1, Unchanged: 2, SkippedRecords: 3,
		FailedSources: 1, LastError: "/sessions/changed.jsonl: unsupported content shape",
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runDoctor(store, []provider.Provider{doctorProvider{revision: 3}}, "/cache/index.db", config.Config{}, "", &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"test-provider\tavailable\t0 indexed\tparser=v3\trefresh=degraded",
		"scanned=4 changed=1 unchanged=2 skipped=3 failed=1",
		"warning\t/sessions/changed.jsonl: unsupported content shape",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output.String())
		}
	}
}

func TestDoctorReportsPendingParserRevision(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	store, err := index.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.ApplyRefresh(context.Background(), "test-provider", 2, nil, index.RefreshStats{Scanned: 4}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runDoctor(store, []provider.Provider{doctorProvider{revision: 3}}, "/cache/index.db", config.Config{}, "", &output); err != nil {
		t.Fatal(err)
	}
	want := "test-provider\tavailable\t0 indexed\tparser=v3\trefresh=pending\tprevious_parser=v2"
	if !strings.Contains(output.String(), want) {
		t.Fatalf("doctor output missing %q:\n%s", want, output.String())
	}
}
