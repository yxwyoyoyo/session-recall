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
	"strings"
	"time"

	"github.com/yxwyoyoyo/session-try/internal/session"
)

type Codex struct {
	Home       string
	Executable string
}

func NewCodex(home string) *Codex {
	return &Codex{Home: home, Executable: "codex"}
}

func (p *Codex) Name() string { return "codex" }

func (p *Codex) root() string { return filepath.Join(p.Home, ".codex") }

func (p *Codex) Available() bool {
	_, commandErr := exec.LookPath(p.Executable)
	_, sessionErr := os.Stat(filepath.Join(p.root(), "sessions"))
	return commandErr == nil && sessionErr == nil
}

type codexTitle struct {
	ID         string    `json:"id"`
	ThreadName string    `json:"thread_name"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (p *Codex) titles() map[string]codexTitle {
	result := map[string]codexTitle{}
	file, err := os.Open(filepath.Join(p.root(), "session_index.jsonl"))
	if err != nil {
		return result
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var title codexTitle
		if json.Unmarshal(scanner.Bytes(), &title) == nil && title.ID != "" {
			result[title.ID] = title
		}
	}
	return result
}

func (p *Codex) Discover(ctx context.Context, known map[string]int64) ([]session.Session, error) {
	titles := p.titles()
	var result []session.Session
	err := filepath.WalkDir(filepath.Join(p.root(), "sessions"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stamp := info.ModTime().UnixNano() ^ info.Size()
		if known[path] == stamp {
			return nil
		}
		item, err := parseCodexFile(path, stamp, titles)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if item.ID != "" {
			result = append(result, item)
		}
		return nil
	})
	return result, err
}

func parseCodexFile(path string, stamp int64, titles map[string]codexTitle) (session.Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return session.Session{}, err
	}
	defer file.Close()

	item := session.Session{Provider: "codex", Source: path, Stamp: stamp}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		var row struct {
			Type      string         `json:"type"`
			Timestamp time.Time      `json:"timestamp"`
			Payload   map[string]any `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		if row.Type == "session_meta" {
			item.ID, _ = row.Payload["id"].(string)
			if item.ID == "" {
				item.ID, _ = row.Payload["session_id"].(string)
			}
			item.Directory, _ = row.Payload["cwd"].(string)
			if raw, ok := row.Payload["timestamp"].(string); ok {
				item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, raw)
			}
		}
		payloadType, _ := row.Payload["type"].(string)
		if row.Type == "event_msg" && payloadType == "user_message" {
			if text, _ := row.Payload["message"].(string); strings.TrimSpace(text) != "" {
				item.Content += text + "\n"
				if item.Title == "" {
					item.Title = oneLine(text, 100)
				}
			}
		}
		if row.Timestamp.After(item.UpdatedAt) {
			item.UpdatedAt = row.Timestamp
		}
	}
	if err := scanner.Err(); err != nil {
		return session.Session{}, err
	}
	if title, ok := titles[item.ID]; ok {
		if strings.TrimSpace(title.ThreadName) != "" {
			item.Title = title.ThreadName
		}
		if title.UpdatedAt.After(item.UpdatedAt) {
			item.UpdatedAt = title.UpdatedAt
		}
	}
	if item.Title == "" {
		item.Title = "Codex session"
	}
	return item, nil
}

func (p *Codex) ResumeCommand(s session.Session) (*exec.Cmd, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("Codex session has no ID")
	}
	cmd := exec.Command(p.Executable, "resume", s.ID, "-C", s.Directory)
	cmd.Dir = s.Directory
	return cmd, nil
}
