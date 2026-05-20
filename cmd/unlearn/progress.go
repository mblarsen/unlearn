package unlearn

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

type inventoryProgress struct {
	Step    string
	Current int
	Total   int
	Detail  string
	Done    bool
}

type auditProgressPrinter struct {
	out     io.Writer
	bar     progress.Model
	frames  []string
	frame   int
	label   lipgloss.Style
	detail  lipgloss.Style
	check   lipgloss.Style
	spinner lipgloss.Style
	muted   lipgloss.Style
}

func newAuditProgressPrinter(out io.Writer) *auditProgressPrinter {
	bar := progress.New(progress.WithWidth(28), progress.WithDefaultGradient())
	return &auditProgressPrinter{
		out:     out,
		bar:     bar,
		frames:  spinner.MiniDot.Frames,
		label:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")),
		detail:  lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		check:   lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		spinner: lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
}

func (p *auditProgressPrinter) Update(event inventoryProgress) {
	if p == nil || p.out == nil {
		return
	}
	label := progressLabel(event.Step)
	if event.Done {
		fmt.Fprintf(p.out, "%s %s %s\n", p.check.Render("✓"), p.label.Render(label), p.detailText(event))
		return
	}
	frame := "•"
	if len(p.frames) > 0 {
		frame = p.frames[p.frame%len(p.frames)]
		p.frame++
	}
	if event.Total > 0 {
		percent := float64(event.Current) / float64(event.Total)
		fmt.Fprintf(p.out, "%s %s %s %s %s\n", p.spinner.Render(frame), p.label.Render(label), p.bar.ViewAs(percent), p.muted.Render(fmt.Sprintf("%d/%d", event.Current, event.Total)), p.detailText(event))
		return
	}
	fmt.Fprintf(p.out, "%s %s %s\n", p.spinner.Render(frame), p.label.Render(label), p.detailText(event))
}

func (p *auditProgressPrinter) detailText(event inventoryProgress) string {
	detail := strings.TrimSpace(event.Detail)
	if detail == "" {
		return ""
	}
	return p.detail.Render("— " + detail)
}

func progressLabel(step string) string {
	switch step {
	case "scan-roots":
		return "Scan skill roots"
	case "history":
		return "Scan history evidence"
	case "analysis":
		return "Run deterministic checks"
	case "llm-summary":
		return "Generate Gemini summaries"
	case "llm-overlap":
		return "Find semantic overlaps"
	default:
		return step
	}
}
