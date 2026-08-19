package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/yxwyoyoyo/session-recall/internal/index"
	"github.com/yxwyoyoyo/session-recall/internal/session"
)

type Picker struct {
	store    *index.Store
	filters  index.Filters
	input    textinput.Model
	results  []session.Match
	cursor   int
	width    int
	height   int
	selected *session.Session
	err      error
}

func Choose(store *index.Store, initial string, filters index.Filters) (*session.Session, error) {
	input := textinput.New()
	input.Placeholder = "Search session content, title, or directory"
	input.SetValue(initial)
	input.Focus()
	input.CharLimit = 300
	picker := &Picker{store: store, filters: filters, input: input, width: 100, height: 30}
	picker.search()
	program := tea.NewProgram(picker)
	final, err := program.Run()
	if err != nil {
		return nil, err
	}
	result := final.(*Picker)
	return result.selected, result.err
}

func (p *Picker) Init() tea.Cmd { return textinput.Blink }

func (p *Picker) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return p, tea.Quit
		case "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "down", "ctrl+n":
			if p.cursor+1 < len(p.results) {
				p.cursor++
			}
			return p, nil
		case "enter":
			if len(p.results) > 0 {
				chosen := p.results[p.cursor].Session
				p.selected = &chosen
			}
			return p, tea.Quit
		}
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(message)
	if p.input.Value() != before {
		p.cursor = 0
		p.search()
	}
	return p, cmd
}

func (p *Picker) search() {
	p.results, p.err = p.store.Search(context.Background(), p.input.Value(), p.filters)
	if p.cursor >= len(p.results) {
		p.cursor = max(0, len(p.results)-1)
	}
}

func (p *Picker) View() string {
	if p.err != nil {
		return fmt.Sprintf("session-recall\n\n%s\n\nEsc cancel\n", p.err)
	}
	var out strings.Builder
	out.WriteString("session-recall  ")
	out.WriteString(p.input.View())
	out.WriteString("\n\n")
	visible := max(1, p.height-6)
	narrow := p.width < wideWidth
	if narrow {
		visible = max(1, visible/2)
	}
	start := 0
	if p.cursor >= visible {
		start = p.cursor - visible + 1
	}
	end := min(len(p.results), start+visible)
	snippetTop := min(snippetTopN, visible/3)
	tokens := strings.Fields(strings.ToLower(p.input.Value()))
	for i := start; i < end; i++ {
		item := p.results[i]
		marker := "  "
		if i == p.cursor {
			marker = "\x1b[1;33m→ \x1b[0m"
		}
		age := index.FormatAge(item.UpdatedAt, time.Now())
		dir := highlight(compactHome(item.Directory), tokens, "\x1b[0m\x1b[1;33m", "\x1b[0m\x1b[2m")
		meta := fmt.Sprintf("\x1b[2m%s · %s\x1b[0m", age, dir)
		providerCol := fmt.Sprintf("\x1b[36m%9s\x1b[0m", item.Provider)
		title := highlight(item.Title, tokens, "\x1b[1;33m", "\x1b[0m")
		if narrow {
			out.WriteString(truncate(marker+title, p.width))
			out.WriteByte('\n')
			metaWidth := ansi.StringWidth(meta)
			pad := max(0, p.width-2-metaWidth-9)
			out.WriteString(truncate("  "+meta+strings.Repeat(" ", pad)+providerCol, p.width))
		} else {
			metaWidth := ansi.StringWidth(meta)
			title = truncate(title, max(4, p.width-metaWidth-13))
			pad := max(0, p.width-2-ansi.StringWidth(title)-2-metaWidth-9)
			out.WriteString(marker + title + "  " + meta + strings.Repeat(" ", pad) + providerCol)
		}
		out.WriteByte('\n')
		if (i < snippetTop || i == p.cursor) && item.Snippet != "" {
			snippet := strings.NewReplacer("[", "", "]", "").Replace(item.Snippet)
			snippet = strings.Join(strings.Fields(snippet), " ")
			snippet = highlight(snippet, tokens, "\x1b[0m\x1b[1;33m", "\x1b[0m\x1b[2m")
			out.WriteString(truncate("    \x1b[2m"+snippet+"\x1b[0m", p.width))
			out.WriteByte('\n')
		}
	}
	if len(p.results) == 0 {
		out.WriteString("  No matching sessions\n")
	}
	out.WriteString("\n\x1b[2m↑↓ navigate  Enter resume  Esc cancel\x1b[0m\n")
	return out.String()
}

func compactHome(path string) string {
	// Keeping this UI-only avoids changing the path used to resume.
	if home := homeDir(); home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

var homeDir = func() string { return "" }

func SetHome(path string) { homeDir = func() string { return path } }

const (
	wideWidth   = 80 // single-line right-aligned rows above this width
	snippetTopN = 3  // leading results whose snippet is always shown
)

func truncate(text string, width int) string {
	if width <= 4 || ansi.StringWidth(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "…") + "\x1b[0m"
}

// highlight wraps every case-insensitive occurrence of any token in text
// with open/close styles. Overlapping matches are never double-wrapped.
func highlight(text string, tokens []string, open, close string) string {
	if len(tokens) == 0 || text == "" {
		return text
	}
	lower := strings.ToLower(text)
	matched := false
	for _, tok := range tokens {
		if tok != "" && strings.Contains(lower, tok) {
			matched = true
			break
		}
	}
	if !matched {
		return text
	}
	var out strings.Builder
	out.Grow(len(text) + 16)
	i := 0
	for i < len(lower) {
		best, bestTok := -1, ""
		for _, tok := range tokens {
			if tok == "" {
				continue
			}
			if idx := strings.Index(lower[i:], tok); idx >= 0 && (best == -1 || idx < best) {
				best, bestTok = idx, tok
			}
		}
		if best == -1 {
			out.WriteString(text[i:])
			break
		}
		out.WriteString(text[i : i+best])
		end := i + best + len(bestTok)
		out.WriteString(open)
		out.WriteString(text[i+best : end])
		out.WriteString(close)
		i = end
	}
	return out.String()
}
