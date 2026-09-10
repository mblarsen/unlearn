package tui

import "strings"

// WithStartupNotices keeps non-fatal audit diagnostics reachable inside the
// dashboard without presenting them as errors.
func (m Model) WithStartupNotices(notices []string) Model {
	if len(notices) == 0 {
		return m
	}
	m.setStatus("Audit notice\n" + strings.Join(notices, "\n\n"))
	return m
}

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
