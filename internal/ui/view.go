package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gcg/agenttree/internal/activity"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#44475A"))

	normalItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F8F8F2"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272A4"))

	badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50FA7B"))

	blockedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF5555"))
)

func (m *Model) View() string {
	if m.cfg.Layout == "tabs" {
		return m.renderTabs()
	}
	return m.renderSidebar()
}

func (m *Model) renderSidebar() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("agenttree"))
	b.WriteString("\n\n")

	if len(m.state.Features) == 0 {
		b.WriteString(dimStyle.Render("  No active features.\n  Press (c) to start."))
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("  [+ New] (c)"))
		return b.String()
	}

	for i, f := range m.state.Features {
		isSelected := (f.Name == m.state.SelectedFeature)
		st := m.tracker.State(f.Name)
		icon := statusIcon(st)

		prefix := "  "
		if isSelected {
			prefix = "> "
		}

		line := fmt.Sprintf("%s%s %d. %s", prefix, icon, i+1, f.Name)
		if prSt, ok := m.prStatuses[f.Branch]; ok {
			ci := ""
			if prSt.CIMark != "" {
				ci = " " + prSt.CIMark
			}
			line += " " + badgeStyle.Render(fmt.Sprintf("[%s%s]", prSt.Display, ci))
		}

		if isSelected {
			b.WriteString(selectedItemStyle.Render(line))
		} else {
			b.WriteString(normalItemStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  [+ New] (c)  (q)uit"))
	return b.String()
}

func (m *Model) renderTabs() string {
	var tabs []string

	for i, f := range m.state.Features {
		isSelected := (f.Name == m.state.SelectedFeature)
		st := m.tracker.State(f.Name)
		icon := statusIcon(st)

		tabText := fmt.Sprintf("%s %d:%s", icon, i+1, f.Name)
		if prSt, ok := m.prStatuses[f.Branch]; ok {
			tabText += fmt.Sprintf(" [%s]", prSt.Display)
		}

		if isSelected {
			tabs = append(tabs, selectedItemStyle.Render(" "+tabText+" "))
		} else {
			tabs = append(tabs, normalItemStyle.Render(" "+tabText+" "))
		}
	}

	tabs = append(tabs, dimStyle.Render(" [+] (c) "))
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func statusIcon(st activity.State) string {
	switch st {
	case activity.StateWorking:
		return "⠋"
	case activity.StateBlocked:
		return blockedStyle.Render("🔔")
	case activity.StateDone:
		return "✓"
	case activity.StateStopped:
		return "⏹"
	default:
		return "💤"
	}
}
