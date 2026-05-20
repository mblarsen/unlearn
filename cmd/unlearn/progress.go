package unlearn

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

type inventoryProgress struct {
	Step    string
	Current int
	Total   int
	Detail  string
	Done    bool
}

type auditProgressPrinter struct {
	out      io.Writer
	live     bool
	liveLine bool
	started  map[string]bool
	bar      progress.Model
	frames   []string
	frame    int
	label    lipgloss.Style
	detail   lipgloss.Style
	check    lipgloss.Style
	spinner  lipgloss.Style
	muted    lipgloss.Style
}

func newAuditProgressPrinter(out io.Writer) *auditProgressPrinter {
	bar := progress.New(progress.WithWidth(28), progress.WithDefaultGradient())
	return &auditProgressPrinter{
		out:     out,
		live:    writerIsTerminal(out),
		started: map[string]bool{},
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
	line := p.line(event)
	if !p.live {
		if event.Done {
			fmt.Fprintln(p.out, line)
			return
		}
		if !p.started[event.Step] {
			p.started[event.Step] = true
			fmt.Fprintln(p.out, line)
		}
		return
	}
	if event.Done {
		p.clearLiveLine()
		fmt.Fprintln(p.out, line)
		p.liveLine = false
		return
	}
	fmt.Fprintf(p.out, "\r\033[2K%s", line)
	p.liveLine = true
}

func (p *auditProgressPrinter) line(event inventoryProgress) string {
	label := progressLabel(event.Step)
	if event.Done {
		return strings.TrimSpace(fmt.Sprintf("%s %s %s", p.check.Render("✓"), p.label.Render(label), p.detailText(event)))
	}
	frame := "•"
	if len(p.frames) > 0 {
		frame = p.frames[p.frame%len(p.frames)]
		p.frame++
	}
	if event.Total > 0 {
		percent := float64(event.Current) / float64(event.Total)
		return strings.TrimSpace(fmt.Sprintf("%s %s %s %s %s", p.spinner.Render(frame), p.label.Render(label), p.bar.ViewAs(percent), p.muted.Render(fmt.Sprintf("%d/%d", event.Current, event.Total)), p.detailText(event)))
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", p.spinner.Render(frame), p.label.Render(label), p.detailText(event)))
}

func (p *auditProgressPrinter) clearLiveLine() {
	if p.liveLine {
		fmt.Fprint(p.out, "\r\033[2K")
	}
}

func writerIsTerminal(out io.Writer) bool {
	file, ok := out.(interface{ Fd() uintptr })
	return ok && isatty.IsTerminal(file.Fd())
}

func (p *auditProgressPrinter) detailText(event inventoryProgress) string {
	detail := truncateProgressDetail(strings.TrimSpace(event.Detail), 72)
	if detail == "" {
		return ""
	}
	return p.detail.Render("— " + detail)
}

func truncateProgressDetail(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-1]) + "…"
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
