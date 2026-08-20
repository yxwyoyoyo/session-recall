package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

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
	header := "session-recall  " + p.input.View()
	count := fmt.Sprintf("%d / %d", min(p.cursor+1, len(p.results)), len(p.results))
	if len(p.results) == 0 {
		count = "0 / 0"
	}
	if p.width >= ansi.StringWidth(count)+10 {
		headerWidth := p.width - ansi.StringWidth(count) - 1
		out.WriteString(padToWidth(truncate(header, headerWidth), headerWidth))
		out.WriteString(" \x1b[2m" + count + "\x1b[0m")
	} else {
		out.WriteString(truncate(header, p.width))
	}
	out.WriteString("\n\n")

	mode := layoutForWidth(p.width)
	baseLines := 1
	if mode == layoutNarrow {
		baseLines = 2
	}
	extraLines := 0
	if len(p.results) > 0 {
		if mode == layoutMedium {
			extraLines++
		}
		if p.results[p.cursor].Snippet != "" {
			extraLines++
		}
	}
	visible := max(1, (p.height-6-extraLines)/baseLines)
	start := 0
	if p.cursor >= visible {
		start = p.cursor - visible + 1
	}
	end := min(len(p.results), start+visible)
	tokens := index.FallbackTokens(p.input.Value())
	for i := start; i < end; i++ {
		item := p.results[i]
		marker := "   "
		if i == p.cursor {
			marker = "\x1b[1;33m→  \x1b[0m"
		}
		age := index.FormatAge(item.UpdatedAt, time.Now())
		dir := highlight(compactHome(item.Directory), tokens, "\x1b[0m\x1b[1;33m", "\x1b[0m\x1b[2m")
		title := highlight(item.Title, tokens, "\x1b[1;33m", "\x1b[0m")
		provider := fmt.Sprintf("\x1b[36m%9s\x1b[0m", item.Provider)
		var line string
		switch mode {
		case layoutWide:
			line = wideRow(marker, title, dir, age, provider, p.width)
		case layoutMedium:
			line = mediumRow(marker, title, age, provider, p.width)
		case layoutNarrow:
			line = truncate(marker+title, p.width)
		}
		if i == p.cursor {
			line = selectedRow(line, p.width)
		}
		out.WriteString(line)
		out.WriteByte('\n')

		if mode == layoutNarrow {
			meta := item.Provider + " · " + age + " · " + compactHome(item.Directory)
			if i == p.cursor {
				meta = item.Provider + " · " + matchSource(item, tokens) + " · " + compactHome(item.Directory)
			}
			out.WriteString("   \x1b[2m" + truncate(meta, max(0, p.width-3)) + "\x1b[0m")
			out.WriteByte('\n')
		} else if mode == layoutMedium && i == p.cursor {
			meta := compactHome(item.Directory) + " · " + matchSource(item, tokens)
			out.WriteString("    \x1b[2m" + truncate(meta, max(0, p.width-4)) + "\x1b[0m")
			out.WriteByte('\n')
		}

		if i == p.cursor && item.Snippet != "" {
			snippet := strings.Join(strings.Fields(item.Snippet), " ")
			snippet = highlight(snippet, tokens, "\x1b[0m\x1b[1;33m", "\x1b[0m\x1b[38;5;249m")
			out.WriteString(truncate("    \x1b[38;5;249m"+snippet+"\x1b[0m", p.width))
			out.WriteByte('\n')
		}
	}
	if len(p.results) == 0 {
		out.WriteString("  No matching sessions\n")
	}
	out.WriteString("\n\x1b[2m↑↓ navigate  Enter resume  Esc cancel\x1b[0m\n")
	return out.String()
}

type pickerLayout int

const (
	layoutNarrow pickerLayout = iota
	layoutMedium
	layoutWide
)

func layoutForWidth(width int) pickerLayout {
	switch {
	case width >= 90:
		return layoutWide
	case width >= 55:
		return layoutMedium
	default:
		return layoutNarrow
	}
}

func wideRow(marker, title, dir, age, provider string, width int) string {
	available := max(0, width-25) // marker + separators + age + provider
	dirWidth := min(24, max(16, available/3))
	dirWidth = min(dirWidth, available)
	titleWidth := max(0, available-dirWidth)
	titleCol := padToWidth(truncate(title, titleWidth), titleWidth)
	dirCol := "\x1b[2m" + padToWidth(truncate(dir, dirWidth), dirWidth) + "\x1b[0m"
	ageCol := fmt.Sprintf("\x1b[2m%10s\x1b[0m", age)
	return truncate(marker+titleCol+" "+dirCol+" "+ageCol+" "+provider, width)
}

func mediumRow(marker, title, age, provider string, width int) string {
	titleWidth := max(0, width-24) // marker + separators + age + provider
	titleCol := padToWidth(truncate(title, titleWidth), titleWidth)
	ageCol := fmt.Sprintf("\x1b[2m%10s\x1b[0m", age)
	return truncate(marker+titleCol+" "+ageCol+" "+provider, width)
}

func padToWidth(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

// selectedRow uses reverse video so selection remains visible on both light
// and dark terminals. Restore it after embedded resets from highlights/colors.
func selectedRow(line string, width int) string {
	line = padToWidth(truncate(line, width), width)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m\x1b[7m")
	return "\x1b[7m" + line + "\x1b[0m"
}

func matchSource(item session.Match, tokens []string) string {
	switch {
	case containsAllTokens(item.Title, tokens):
		return "title match"
	case containsAllTokens(item.Directory, tokens):
		return "path match"
	case containsAllTokens(item.Snippet, tokens):
		return "content match"
	case item.Snippet != "":
		return "mixed match"
	default:
		return "recent"
	}
}

func containsAllTokens(text string, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	text = index.FoldDiacritics(strings.ToLower(text))
	for _, token := range tokens {
		if token != "" && !strings.Contains(text, token) {
			return false
		}
	}
	return true
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

func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "…") + "\x1b[0m"
}

// highlight wraps every case-insensitive occurrence of any query token in
// text with open/close styles. Matching is diacritic-insensitive like FTS
// (tokens already arrive folded via index.FallbackTokens); the original
// text keeps its accents. Overlapping matches are never double-wrapped.
func highlight(text string, tokens []string, open, close string) string {
	if len(tokens) == 0 || text == "" {
		return text
	}
	orig := []rune(text)
	folded, origIdx := foldIndices(orig)
	var foldBuf strings.Builder
	foldBuf.Grow(len(text))
	pos := make([]int, len(folded))
	for fi, r := range folded {
		pos[fi] = foldBuf.Len()
		foldBuf.WriteRune(unicode.ToLower(r))
	}
	flat := foldBuf.String()
	matched := false
	for _, tok := range tokens {
		if tok != "" && strings.Contains(flat, tok) {
			matched = true
			break
		}
	}
	if !matched {
		return text
	}
	origOffs := make([]int, len(orig))
	for i := 1; i < len(orig); i++ {
		origOffs[i] = origOffs[i-1] + len(string(orig[i-1]))
	}
	// byteToOrig maps each byte offset in flat to the byte offset in text.
	// Positions are captured from the builder before each rune is written,
	// so lowercasing that changes byte length (e.g. ẞ -> ß) stays exact.
	byteToOrig := make([]int, len(flat)+1)
	for i := range byteToOrig {
		byteToOrig[i] = -1
	}
	for fi := range folded {
		byteToOrig[pos[fi]] = origOffs[origIdx[fi]]
	}
	byteToOrig[len(flat)] = len(text)
	var out strings.Builder
	out.Grow(len(text) + 16)
	i := 0
	for i < len(flat) {
		best, bestEnd := -1, 0
		for _, tok := range tokens {
			if tok == "" {
				continue
			}
			if idx := strings.Index(flat[i:], tok); idx >= 0 && (best == -1 || idx < best) {
				best, bestEnd = idx, i+idx+len(tok)
			}
		}
		if best == -1 {
			out.WriteString(text[byteToOrig[i]:])
			break
		}
		out.WriteString(text[byteToOrig[i]:byteToOrig[i+best]])
		if s := text[byteToOrig[i+best]:byteToOrig[bestEnd]]; s != "" {
			out.WriteString(open)
			out.WriteString(s)
			out.WriteString(close)
		}
		i = bestEnd
	}
	return out.String()
}

// foldIndices returns the diacritic-folded forms of the runes in rs (NFD
// with combining marks dropped, semantics shared with index.FoldDiacritics)
// and, for each folded rune, the index of its original rune in rs.
func foldIndices(rs []rune) (folded []rune, origIdx []int) {
	folded = make([]rune, 0, len(rs))
	origIdx = make([]int, 0, len(rs))
	for i, r := range rs {
		for _, d := range norm.NFD.String(string(r)) {
			if unicode.Is(unicode.Mn, d) {
				continue
			}
			folded = append(folded, d)
			origIdx = append(origIdx, i)
		}
	}
	return folded, origIdx
}
