package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

// Kiro reads the stable Kiro CLI v2 session triplets stored below
// $KIRO_HOME/sessions/cli. KIRO_HOME defaults to ~/.kiro.
type Kiro struct {
	Home       string
	Executable string
}

func NewKiro(userHome string) *Kiro {
	home := filepath.Join(userHome, ".kiro")
	if configured := strings.TrimSpace(os.Getenv("KIRO_HOME")); configured != "" {
		home = configured
	}
	return &Kiro{Home: home, Executable: "kiro-cli"}
}

func (p *Kiro) Name() string { return "kiro" }

func (p *Kiro) ParserRevision() int { return 1 }

func (p *Kiro) sessionsDir() string {
	return filepath.Join(p.Home, "sessions", "cli")
}

func (p *Kiro) Available() bool {
	_, commandErr := exec.LookPath(p.Executable)
	_, sessionsErr := os.Stat(p.sessionsDir())
	return commandErr == nil && sessionsErr == nil
}

type kiroMetadata struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Title     string `json:"title"`
}

type kiroEnvelope struct {
	Kind string `json:"kind"`
	Data struct {
		Content []struct {
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
		} `json:"content"`
	} `json:"data"`
}

func (p *Kiro) Discover(ctx context.Context, known map[string]int64) (Discovery, error) {
	report := Discovery{}
	anchors, err := kiroAnchors(p.sessionsDir())
	if err != nil {
		return report, err
	}
	ids := make([]string, 0, len(anchors))
	for id := range anchors {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		report.Scanned++
		if err := ctx.Err(); err != nil {
			return report, err
		}
		anchor := anchors[id]
		metaPath := filepath.Join(p.sessionsDir(), id+".json")
		journalPath := filepath.Join(p.sessionsDir(), id+".jsonl")
		stamp, fallbackTime, err := kiroStamp(metaPath, journalPath)
		if err != nil {
			report.Failures = append(report.Failures, SourceFailure{Source: anchor, Err: fmt.Errorf("inspect Kiro session %s: %w", id, err)})
			continue
		}
		if known[anchor] == stamp {
			report.Unchanged++
			continue
		}
		item, skipped, err := parseKiroSession(metaPath, journalPath, id)
		report.SkippedRecords += skipped
		if err != nil {
			report.Failures = append(report.Failures, SourceFailure{Source: anchor, Err: fmt.Errorf("parse Kiro session %s: %w", id, err)})
			continue
		}
		item.Provider = p.Name()
		item.Source = anchor
		item.Stamp = stamp
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = fallbackTime
		}
		report.Sessions = append(report.Sessions, item)
	}
	return report, nil
}

func kiroAnchors(dir string) (map[string]string, error) {
	anchors := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return anchors, nil
		}
		return nil, err
	}
	// Metadata is the canonical anchor. Journals without metadata are tolerated.
	for _, extension := range []string{".json", ".jsonl"} {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), extension)
			if id == "" {
				continue
			}
			if _, exists := anchors[id]; !exists {
				anchors[id] = filepath.Join(dir, entry.Name())
			}
		}
	}
	return anchors, nil
}

func kiroStamp(paths ...string) (int64, time.Time, error) {
	var stamp int64
	var latest time.Time
	found := false
	for i, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, time.Time{}, err
		}
		found = true
		component := info.ModTime().UnixNano() ^ (info.Size() << (i * 7))
		stamp ^= component
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	if !found {
		return 0, time.Time{}, fs.ErrNotExist
	}
	return stamp, latest, nil
}

func parseKiroSession(metaPath, journalPath, fallbackID string) (session.Session, int, error) {
	item := session.Session{ID: fallbackID}
	skipped := 0
	incompatible := false
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta kiroMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			return session.Session{}, skipped, fmt.Errorf("decode metadata: %w", err)
		}
		if meta.SessionID != "" {
			item.ID = meta.SessionID
		}
		if meta.SessionID == "" || meta.CWD == "" {
			return session.Session{}, skipped, fmt.Errorf("metadata has no session identity or working directory")
		}
		item.Directory = meta.CWD
		item.Title = strings.TrimSpace(meta.Title)
		if meta.UpdatedAt != "" {
			item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, meta.UpdatedAt)
		}
	} else if !os.IsNotExist(err) {
		return session.Session{}, skipped, err
	}

	file, err := os.Open(journalPath)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
		for scanner.Scan() {
			if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
				continue
			}
			var envelope kiroEnvelope
			if json.Unmarshal(scanner.Bytes(), &envelope) != nil {
				skipped++
				incompatible = true
				continue
			}
			if envelope.Kind != "Prompt" {
				continue
			}
			if len(envelope.Data.Content) == 0 {
				skipped++
				incompatible = true
				continue
			}
			for _, part := range envelope.Data.Content {
				if part.Kind != "text" {
					skipped++
					incompatible = true
					continue
				}
				var text string
				if json.Unmarshal(part.Data, &text) != nil {
					skipped++
					incompatible = true
					continue
				}
				if strings.TrimSpace(text) == "" {
					continue
				}
				item.Content += text + "\n"
				if item.Title == "" {
					item.Title = oneLine(text, 100)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return session.Session{}, skipped, fmt.Errorf("read journal: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return session.Session{}, skipped, err
	}
	if incompatible {
		return session.Session{}, skipped, fmt.Errorf("unsupported or malformed Kiro prompt record")
	}
	if item.Title == "" {
		item.Title = "Kiro session"
	}
	return item, skipped, nil
}

func (p *Kiro) ResumeCommand(s session.Session) (*exec.Cmd, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("Kiro session has no ID")
	}
	cmd := exec.Command(p.Executable, "chat", "--resume-id", s.ID)
	cmd.Dir = s.Directory
	return cmd, nil
}
