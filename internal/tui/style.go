// Package tui provides bubbletea-based terminal UI components for departai.
package tui

import "github.com/charmbracelet/lipgloss"

// Styles for the agent turn view, matching departai's cyan/green palette.
var (
	styleCyan           = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleBold           = lipgloss.NewStyle().Bold(true)
	styleBoldCyn        = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	styleGreen          = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	styleFaint          = lipgloss.NewStyle().Faint(true)
	styleDiffAdd        = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleDiffDel        = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleRule           = lipgloss.NewStyle().Faint(true)
	styleSelectedMarker = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true) // yellow ▹
	styleSelectedTool   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true) // yellow title
)

const rule = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
