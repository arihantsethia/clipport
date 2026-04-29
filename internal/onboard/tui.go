package onboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tuiStep int

const (
	stepName tuiStep = iota
	stepSelect
	stepReview
)

type TUIResult struct {
	Groups []HostGroup
}

type TUIModel struct {
	hosts    []SSHHost
	filtered []int

	cursor   int
	selected map[int]bool
	groups   []HostGroup

	step        tuiStep
	filterMode  bool
	filter      string
	input       string
	message     string
	messageGood bool
	quitting    bool
}

var (
	frameStyle    = lipgloss.NewStyle().Padding(1, 2)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	keyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	activeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62")).Padding(0, 1)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cardStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
)

func RunTUI(hosts []SSHHost) (TUIResult, error) {
	m := NewTUIModel(hosts)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return TUIResult{}, err
	}
	model := final.(TUIModel)
	if model.quitting {
		return TUIResult{}, fmt.Errorf("onboarding cancelled")
	}
	return TUIResult{Groups: model.groups}, nil
}

func NewTUIModel(hosts []SSHHost) TUIModel {
	m := TUIModel{hosts: hosts, selected: map[int]bool{}}
	m.rebuildFilter()
	return m
}

func (m TUIModel) Init() tea.Cmd { return nil }

func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.filterMode {
		return m.updateFilter(key)
	}
	switch key.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	}
	switch m.step {
	case stepName:
		return m.updateName(key)
	case stepSelect:
		return m.updateSelect(key)
	case stepReview:
		return m.updateReview(key)
	default:
		return m, nil
	}
}

func (m TUIModel) updateFilter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "enter":
		m.filterMode = false
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.rebuildFilter()
		}
	default:
		if len(key.String()) == 1 {
			m.filter += key.String()
			m.rebuildFilter()
		}
	}
	return m, nil
}

func (m TUIModel) updateSelect(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "/":
		m.filterMode = true
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case " ":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.cursor]
			m.selected[idx] = !m.selected[idx]
			m.message = ""
		}
	case "enter":
		routes := m.selectedRoutes()
		if len(routes) == 0 {
			m.setError("select at least one route to reach " + m.input)
			return m, nil
		}
		m.groups = append(m.groups, HostGroup{Name: strings.TrimSpace(m.input), Routes: routes})
		m.selected = map[int]bool{}
		m.filter = ""
		m.rebuildFilter()
		m.setOK("added machine " + strings.TrimSpace(m.input))
		m.input = ""
		m.step = stepName
	case "r":
		m.selected = map[int]bool{}
		m.setOK("selection reset")
	case "d":
		if len(m.groups) == 0 {
			m.setError("add at least one machine before writing config")
			return m, nil
		}
		m.message = ""
		m.step = stepReview
	}
	return m, nil
}

func (m TUIModel) updateName(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		name := strings.TrimSpace(m.input)
		if name == "" {
			m.setError("machine name cannot be empty")
			return m, nil
		}
		if m.hasGroup(name) {
			m.setError("machine " + name + " already exists")
			return m, nil
		}
		m.step = stepSelect
		m.message = ""
	case "esc":
		if len(m.groups) > 0 {
			m.step = stepReview
		}
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		if len(key.String()) == 1 {
			m.input += key.String()
		}
	}
	return m, nil
}

func (m TUIModel) updateReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter", "w":
		return m, tea.Quit
	case "esc":
		m.step = stepSelect
	}
	return m, nil
}

func (m TUIModel) View() string {
	if m.quitting {
		return "cancelled\n"
	}
	var body string
	switch m.step {
	case stepName:
		body = m.viewName()
	case stepSelect:
		body = m.viewSelect()
	case stepReview:
		body = m.viewReview()
	}
	return frameStyle.Render(lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(), body))
}

func (m TUIModel) sidebar() string {
	steps := []struct {
		label string
		step  tuiStep
	}{
		{"1 Name machine", stepName},
		{"2 Pick routes", stepSelect},
		{"3 Review", stepReview},
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("clipport") + "\n")
	b.WriteString(subtleStyle.Render("clipboard files over SSH") + "\n\n")
	for _, s := range steps {
		line := s.label
		if m.step == s.step {
			line = activeStyle.Render(line)
		} else {
			line = subtleStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s %d\n", subtleStyle.Render("ssh aliases"), len(m.hosts)))
	b.WriteString(fmt.Sprintf("%s %d\n", subtleStyle.Render("machines"), len(m.groups)))
	return lipgloss.NewStyle().Width(28).Render(b.String())
}

func (m TUIModel) viewSelect() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Pick routes for "+strings.TrimSpace(m.input)) + "\n")
	b.WriteString(subtleStyle.Render("Select SSH aliases that reach the same remote filesystem. Do not mix different machines here.") + "\n")
	b.WriteString(subtleStyle.Render("Order follows the SSH config list; first selected route has highest priority.") + "\n\n")
	b.WriteString(m.filterLine() + "\n\n")
	if len(m.filtered) == 0 {
		b.WriteString(errorStyle.Render("No SSH aliases match this filter.") + "\n")
	} else {
		for row, idx := range m.filtered {
			h := m.hosts[idx]
			cursor := " "
			if row == m.cursor {
				cursor = keyStyle.Render(">")
			}
			check := "[ ]"
			if m.selected[idx] {
				check = selectedStyle.Render("[x]")
			}
			target := h.Target()
			b.WriteString(fmt.Sprintf("%s %s %-24s %s\n", cursor, check, h.Alias, subtleStyle.Render(target)))
		}
	}
	b.WriteString("\n" + m.selectedSummary())
	b.WriteString("\n" + m.configuredSummary())
	b.WriteString("\n" + m.help("space toggle", "/ filter", "enter add machine", "d review", "r reset", "q quit"))
	b.WriteString(m.messageView())
	return cardStyle.Width(86).Render(b.String())
}

func (m TUIModel) viewName() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Create a remote machine") + "\n")
	b.WriteString(subtleStyle.Render("A machine means one remote filesystem. Routes are the SSH aliases you use to reach it.") + "\n")
	b.WriteString(subtleStyle.Render("Example: machine devbox uses routes devbox-lan and devbox-public.") + "\n\n")
	b.WriteString("Machine name\n")
	b.WriteString(activeStyle.Render(" "+m.input+" ") + "\n\n")
	if len(m.groups) > 0 {
		b.WriteString(m.configuredSummary() + "\n\n")
	}
	b.WriteString(m.help("type name", "enter pick routes", "esc review", "q quit"))
	b.WriteString(m.messageView())
	return cardStyle.Width(86).Render(b.String())
}

func (m TUIModel) viewReview() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Review machines and routes") + "\n")
	b.WriteString(subtleStyle.Render("Passwordless SSH is required. The daemon will keep route health warm in the background.") + "\n\n")
	for _, g := range m.groups {
		b.WriteString(successStyle.Render(g.Name) + subtleStyle.Render("  /tmp/clipport/<user>/...") + "\n")
		for i, route := range g.Routes {
			b.WriteString(fmt.Sprintf("  priority %-2d %s\n", (i+1)*10, route))
		}
		b.WriteString("\n")
	}
	b.WriteString(m.help("enter/w write config", "esc add another machine", "q quit"))
	b.WriteString(m.messageView())
	return cardStyle.Width(86).Render(b.String())
}

func (m TUIModel) filterLine() string {
	prefix := "Filter"
	if m.filterMode {
		prefix = keyStyle.Render("Filter")
	}
	value := m.filter
	if value == "" {
		value = subtleStyle.Render("type / to search aliases, users, or hostnames")
	}
	return fmt.Sprintf("%s  %s", prefix, value)
}

func (m TUIModel) selectedSummary() string {
	routes := m.selectedRoutes()
	if len(routes) == 0 {
		return subtleStyle.Render("No routes selected yet.")
	}
	return successStyle.Render("Selected routes: ") + strings.Join(routes, " -> ")
}

func (m TUIModel) configuredSummary() string {
	if len(m.groups) == 0 {
		return subtleStyle.Render("No machines configured yet.")
	}
	var lines []string
	for _, g := range m.groups {
		lines = append(lines, fmt.Sprintf("%s (%d routes)", g.Name, len(g.Routes)))
	}
	return successStyle.Render("Configured: ") + strings.Join(lines, ", ")
}

func (m TUIModel) help(parts ...string) string {
	var rendered []string
	for _, p := range parts {
		rendered = append(rendered, keyStyle.Render(p))
	}
	return strings.Join(rendered, subtleStyle.Render(" • ")) + "\n"
}

func (m TUIModel) messageView() string {
	if m.message == "" {
		return ""
	}
	style := errorStyle
	if m.messageGood {
		style = successStyle
	}
	return "\n" + style.Render(m.message) + "\n"
}

func (m *TUIModel) rebuildFilter() {
	m.filtered = m.filtered[:0]
	query := strings.ToLower(strings.TrimSpace(m.filter))
	for i, h := range m.hosts {
		haystack := strings.ToLower(h.Alias + " " + h.User + " " + h.HostName)
		if query == "" || strings.Contains(haystack, query) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m TUIModel) selectedRoutes() []string {
	var routes []string
	for idx := range m.hosts {
		if m.selected[idx] {
			routes = append(routes, m.hosts[idx].Alias)
		}
	}
	return routes
}

func (m TUIModel) hasGroup(name string) bool {
	for _, g := range m.groups {
		if g.Name == name {
			return true
		}
	}
	return false
}

func (m *TUIModel) setError(message string) {
	m.message = message
	m.messageGood = false
}

func (m *TUIModel) setOK(message string) {
	m.message = message
	m.messageGood = true
}

func (h SSHHost) Target() string {
	target := h.HostName
	if target == "" {
		target = h.Alias
	}
	if h.User != "" {
		target = h.User + "@" + target
	}
	return target
}

func DefaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "clipport", "config.toml")
}
