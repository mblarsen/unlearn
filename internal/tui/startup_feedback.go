package tui

import "strings"

// WithStartupWarnings keeps audit diagnostics reachable inside the dashboard,
// rather than only printing them behind its alternate screen.
func (m Model) WithStartupWarnings(warnings []string) Model {
	if len(warnings) == 0 {
		return m
	}
	m.setStatus("Audit needs attention\n" + strings.Join(warnings, "\n\n"))
	m.StatusError = true
	m.StatusRecovery = "Review the details. LLM review may be incomplete; available findings remain usable."
	return m
}
