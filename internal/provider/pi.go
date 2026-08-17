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
	"strings"
	"time"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

// Pi reads coding-agent JSONL sessions without modifying Pi's session tree.
type Pi struct {
	AgentDir   string
	SessionDir string
	Executable string
}

func NewPi(userHome string) *Pi {
	agentDir := filepath.Join(userHome, ".pi", "agent")
	if configured := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); configured != "" {
		agentDir = expandPiHome(configured, userHome)
	}
	sessionDir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_SESSION_DIR"))
	if sessionDir == "" {
		sessionDir = piConfiguredSessionDir(agentDir)
	}
	sessionDir = expandPiHome(sessionDir, userHome)
	if sessionDir == "" {
		sessionDir = filepath.Join(agentDir, "sessions")
	} else if !filepath.IsAbs(sessionDir) {
		sessionDir = filepath.Join(agentDir, sessionDir)
	}
	return &Pi{AgentDir: agentDir, SessionDir: sessionDir, Executable: "pi"}
}

func expandPiHome(path, userHome string) string {
	if path == "~" {
		return userHome
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(userHome, path[2:])
	}
	return path
}

func piConfiguredSessionDir(agentDir string) string {
	data, err := os.ReadFile(filepath.Join(agentDir, "settings.json"))
	if err != nil {
		return ""
	}
	var settings struct {
		SessionDir string `json:"sessionDir"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return ""
	}
	return strings.TrimSpace(settings.SessionDir)
}

func (p *Pi) Name() string { return "pi" }

func (p *Pi) ParserRevision() int { return 1 }

func (p *Pi) Available() bool {
	_, err := exec.LookPath(p.Executable)
	return err == nil
}

func (p *Pi) Discover(ctx context.Context, known map[string]int64) (Discovery, error) {
	report := Discovery{}
	err := filepath.WalkDir(p.SessionDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		report.Scanned++
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stamp := info.ModTime().UnixNano() ^ info.Size()
		if known[path] == stamp {
			report.Unchanged++
			return nil
		}
		item, skipped, err := parsePiFile(path, stamp)
		report.SkippedRecords += skipped
		if err != nil {
			report.Failures = append(report.Failures, SourceFailure{Source: path, Err: err})
			return nil
		}
		if item.ID != "" {
			report.Sessions = append(report.Sessions, item)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return report, nil
	}
	return report, err
}

type piEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	CWD       string          `json:"cwd"`
	Name      string          `json:"name"`
	Message   json.RawMessage `json:"message"`
}

type piMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Timestamp int64           `json:"timestamp"`
}

func parsePiFile(path string, stamp int64) (session.Session, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return session.Session{}, 0, err
	}
	defer file.Close()

	item := session.Session{Provider: "pi", Source: path, Stamp: stamp}
	skipped := 0
	incompatible := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var entry piEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			skipped++
			incompatible = true
			continue
		}
		if updated, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil && updated.After(item.UpdatedAt) {
			item.UpdatedAt = updated
		}
		switch entry.Type {
		case "session":
			item.ID = entry.ID
			item.Directory = entry.CWD
		case "session_info":
			if name := strings.TrimSpace(entry.Name); name != "" {
				item.Title = name
			}
		case "message":
			var message piMessage
			if json.Unmarshal(entry.Message, &message) != nil || message.Role == "" {
				skipped++
				incompatible = true
				continue
			}
			if message.Role != "user" {
				continue
			}
			text, recognized := piTextContent(message.Content)
			if !recognized {
				skipped++
				incompatible = true
				continue
			}
			if text == "" {
				continue
			}
			item.Content += text + "\n"
			if item.Title == "" {
				item.Title = oneLine(text, 100)
			}
			updated := time.UnixMilli(message.Timestamp)
			if updated.After(item.UpdatedAt) {
				item.UpdatedAt = updated
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return session.Session{}, skipped, fmt.Errorf("read Pi session: %w", err)
	}
	if item.ID == "" {
		return session.Session{}, skipped, fmt.Errorf("no recognizable Pi session header")
	}
	if item.Directory == "" {
		return session.Session{}, skipped, fmt.Errorf("Pi session header has no working directory")
	}
	if incompatible {
		return session.Session{}, skipped, fmt.Errorf("unsupported or malformed Pi session record")
	}
	if item.Title == "" {
		item.Title = "Pi session"
	}
	return item, skipped, nil
}

func piTextContent(raw json.RawMessage) (string, bool) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text), true
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			if strings.TrimSpace(part.Text) != "" {
				texts = append(texts, strings.TrimSpace(part.Text))
			}
		case "image":
			// Image prompts are recognized but intentionally not indexed.
		default:
			return "", false
		}
	}
	return strings.Join(texts, "\n"), true
}

func (p *Pi) ResumeCommand(s session.Session) (*exec.Cmd, error) {
	target := s.Source
	if target == "" {
		target = s.ID
	}
	if target == "" {
		return nil, fmt.Errorf("Pi session has no source or ID")
	}
	cmd := exec.Command(p.Executable, "--session", target)
	cmd.Dir = s.Directory
	return cmd, nil
}
