package picker

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"docxedit/internal/docx"
)

var ErrCanceled = errors.New("selection canceled")

// Select opens an interactive searchable multi-select list.
func Select(parts []docx.Part) ([]string, error) {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return nil, fmt.Errorf("interactive selection requires a terminal")
	}
	initialModel := newModel(parts)
	final, err := tea.NewProgram(initialModel).Run()
	if err != nil {
		return nil, fmt.Errorf("run selector: %w", err)
	}
	result, ok := final.(model)
	if !ok {
		return nil, fmt.Errorf("selector returned an unexpected state")
	}
	if result.canceled {
		return nil, ErrCanceled
	}
	selected := make([]string, 0, len(result.selected))
	for _, part := range result.parts {
		if result.selected[part.Name] {
			selected = append(selected, part.Name)
		}
	}
	if len(selected) == 0 {
		return nil, ErrCanceled
	}
	return selected, nil
}

type model struct {
	parts    []docx.Part
	filtered []int
	selected map[string]bool
	filter   string
	cursor   int
	height   int
	message  string
	canceled bool
}

func newModel(parts []docx.Part) model {
	copyOfParts := append([]docx.Part(nil), parts...)
	sort.Slice(copyOfParts, func(i, j int) bool {
		return strings.ToLower(copyOfParts[i].Name) < strings.ToLower(copyOfParts[j].Name)
	})
	result := model{
		parts:    copyOfParts,
		selected: make(map[string]bool),
		height:   20,
	}
	for _, part := range copyOfParts {
		if strings.EqualFold(part.Name, "word/document.xml") {
			result.selected[part.Name] = true
			break
		}
	}
	result.refilter()
	return result
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.height = message.Height
	case tea.KeyPressMsg:
		switch message.String() {
		case "ctrl+c", "esc":
			m.canceled = true
			return m, tea.Quit
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor+1 < len(m.filtered) {
				m.cursor++
			}
		case "space":
			if len(m.filtered) > 0 {
				name := m.parts[m.filtered[m.cursor]].Name
				m.selected[name] = !m.selected[name]
				m.message = ""
			}
		case "enter":
			if selectedCount(m.selected) == 0 {
				m.message = "Select at least one part."
				break
			}
			return m, tea.Quit
		case "backspace", "ctrl+h":
			if m.filter != "" {
				_, size := utf8.DecodeLastRuneInString(m.filter)
				m.filter = m.filter[:len(m.filter)-size]
				m.refilter()
			}
		default:
			if text := message.Key().Text; text != "" && !strings.ContainsAny(text, "\r\n\t") {
				m.filter += text
				m.refilter()
			}
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	var output strings.Builder
	output.WriteString("Select DOCX parts to edit\n")
	output.WriteString("Type to filter · ↑/↓ move · Space toggle · Enter open · Esc cancel\n")
	fmt.Fprintf(&output, "Filter: %s\n\n", m.filter)

	visibleRows := m.height - 7
	if visibleRows < 3 {
		visibleRows = 3
	}
	start := 0
	if m.cursor >= visibleRows {
		start = m.cursor - visibleRows + 1
	}
	end := min(start+visibleRows, len(m.filtered))
	for position := start; position < end; position++ {
		part := m.parts[m.filtered[position]]
		cursor := "  "
		if position == m.cursor {
			cursor = "> "
		}
		mark := "[ ]"
		if m.selected[part.Name] {
			mark = "[x]"
		}
		fmt.Fprintf(&output, "%s%s %-64s %10s\n", cursor, mark, part.Name, humanSize(part.UncompressedSize))
	}
	if len(m.filtered) == 0 {
		output.WriteString("  No matching parts.\n")
	}
	fmt.Fprintf(&output, "\n%d selected", selectedCount(m.selected))
	if m.message != "" {
		fmt.Fprintf(&output, " · %s", m.message)
	}
	view := tea.NewView(output.String())
	view.AltScreen = true
	return view
}

func (m *model) refilter() {
	query := strings.ToLower(m.filter)
	m.filtered = m.filtered[:0]
	for index, part := range m.parts {
		if query == "" || strings.Contains(strings.ToLower(part.Name), query) {
			m.filtered = append(m.filtered, index)
		}
	}
	m.cursor = 0
}

func selectedCount(selected map[string]bool) int {
	count := 0
	for _, value := range selected {
		if value {
			count++
		}
	}
	return count
}

func humanSize(size uint64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	divisor, exponent := uint64(unit), 0
	for quotient := size / unit; quotient >= unit && exponent < 4; quotient /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(divisor), "KMGTPE"[exponent])
}
