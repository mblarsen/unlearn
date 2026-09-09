package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
)

type draftOperationID uint64

type draftLifecycle struct {
	nextID   draftOperationID
	activeID draftOperationID
	cancelFn context.CancelFunc
}

func (l *draftLifecycle) start(service ActionService, skills []inventory.Skill) tea.Cmd {
	l.cancel()
	l.nextID++
	l.activeID = l.nextID
	ctx, cancel := context.WithCancel(context.Background())
	l.cancelFn = cancel
	id := l.activeID
	selected := append([]inventory.Skill(nil), skills...)
	return func() tea.Msg {
		result, err := service.DraftMerge(ctx, selected)
		return draftMergeResultMsg{OperationID: id, Skills: selected, Result: result, Err: err}
	}
}

func (l *draftLifecycle) cancel() {
	if l.cancelFn != nil {
		l.cancelFn()
	}
	l.activeID = 0
	l.cancelFn = nil
}

func (l *draftLifecycle) accept(msg draftMergeResultMsg) bool {
	if msg.OperationID == 0 || msg.OperationID != l.activeID {
		return false
	}
	l.activeID = 0
	l.cancelFn = nil
	return true
}

var errDraftCredentialsRequired = errors.New("GEMINI_API_KEY or GOOGLE_API_KEY is required for preview draft generation")

type unavailableDraftGenerator struct{}

func (unavailableDraftGenerator) GenerateMergedSkillDraft(context.Context, llm.DraftRequest) (llm.DraftResult, error) {
	return llm.DraftResult{}, errDraftCredentialsRequired
}
