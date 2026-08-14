package provider

import (
	"bufio"
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

	"github.com/yxwyoyoyo/session-try/internal/session"
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

func (p *Kiro) Discover(ctx context.Context, known map[string]int64) ([]session.Session, error) {
	anchors, err := kiroAnchors(p.sessionsDir())
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(anchors))
	for id := range anchors {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	result := make([]session.Session, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		anchor := anchors[id]
		metaPath := filepath.Join(p.sessionsDir(), id+".json")
		journalPath := filepath.Join(p.sessionsDir(), id+".jsonl")
		stamp, fallbackTime, err := kiroStamp(metaPath, journalPath)
		if err != nil {
			return nil, fmt.Errorf("inspect Kiro session %s: %w", id, err)
		}
		if known[anchor] == stamp {
			continue
		}
		item, err := parseKiroSession(metaPath, journalPath, id)
		if err != nil {
			return nil, fmt.Errorf("parse Kiro session %s: %w", id, err)
		}
		item.Provider = p.Name()
		item.Source = anchor
		item.Stamp = stamp
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = fallbackTime
		}
		result = append(result, item)
	}
	return result, nil
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

func parseKiroSession(metaPath, journalPath, fallbackID string) (session.Session, error) {
	item := session.Session{ID: fallbackID}
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta kiroMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			return session.Session{}, fmt.Errorf("decode metadata: %w", err)
		}
		if meta.SessionID != "" {
			item.ID = meta.SessionID
		}
		item.Directory = meta.CWD
		item.Title = strings.TrimSpace(meta.Title)
		if meta.UpdatedAt != "" {
			item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, meta.UpdatedAt)
		}
	} else if !os.IsNotExist(err) {
		return session.Session{}, err
	}

	file, err := os.Open(journalPath)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
		for scanner.Scan() {
			var envelope kiroEnvelope
			if json.Unmarshal(scanner.Bytes(), &envelope) != nil || envelope.Kind != "Prompt" {
				continue
			}
			for _, part := range envelope.Data.Content {
				if part.Kind != "text" {
					continue
				}
				var text string
				if json.Unmarshal(part.Data, &text) != nil || strings.TrimSpace(text) == "" {
					continue
				}
				item.Content += text + "\n"
				if item.Title == "" {
					item.Title = oneLine(text, 100)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return session.Session{}, fmt.Errorf("read journal: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return session.Session{}, err
	}
	if item.Title == "" {
		item.Title = "Kiro session"
	}
	return item, nil
}

func (p *Kiro) ResumeCommand(s session.Session) (*exec.Cmd, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("Kiro session has no ID")
	}
	cmd := exec.Command(p.Executable, "chat", "--resume-id", s.ID)
	cmd.Dir = s.Directory
	return cmd, nil
}
