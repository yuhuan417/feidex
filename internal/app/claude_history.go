package app

import (
	"fmt"
	"strings"

	"feidex/internal/app/claudesupport"
	"feidex/internal/app/claudesession"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// History service type and constructor
// ---------------------------------------------------------------------------

type claudeHistoryService = claudesupport.HistoryService

func newClaudeHistoryService(a *App) *claudeHistoryService {
	return &claudeHistoryService{
		FetchClaudeSessionTurns: func(sessionKey string) (*state.Session, *codexrpc.ThreadReadThread, []appruntime.ClaudeHistoryTurnSummary, error) {
			return fetchClaudeCurrentSessionTurns(a, sessionKey)
		},
		ThreadLabel:  currentThreadLabel,
		MenuCardBody: menuCardBody,
		PageSize:     historyPageSize,
	}
}

// ---------------------------------------------------------------------------
// Thin wrappers — delegate to HistoryService
// ---------------------------------------------------------------------------

func historyTurnIndexForOrdinal(a *App, sessionKey string, ordinal int) (int, error) {
	return newClaudeHistoryService(a).HistoryTurnIndexForOrdinal(sessionKey, ordinal)
}

func renderClaudeHistoryCard(a *App, sessionKey string, page int) (map[string]any, error) {
	return newClaudeHistoryService(a).RenderHistoryCard(sessionKey, page)
}

func renderClaudeHistoryDetailCard(a *App, sessionKey string, index int) (map[string]any, error) {
	return newClaudeHistoryService(a).RenderHistoryDetailCard(sessionKey, index, a.feishu.SimpleStatusCard)
}

// ---------------------------------------------------------------------------
// Fetch helper — stays in app/ since it uses app-internal aliases
// ---------------------------------------------------------------------------

func fetchClaudeCurrentSessionTurns(a *App, sessionKey string) (*state.Session, *codexrpc.ThreadReadThread, []appruntime.ClaudeHistoryTurnSummary, error) {
	if a == nil || a.store == nil {
		return nil, nil, nil, fmt.Errorf("store not initialized")
	}
	sess := appState(a).session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, nil, nil, fmt.Errorf("当前没有活动会话")
	}
	filePath, meta, err := claudesession.FindSessionFile(strings.TrimSpace(sess.ActiveThreadID))
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, nil, nil, fmt.Errorf("未找到 Claude session `%s` 的本地 transcript", strings.TrimSpace(sess.ActiveThreadID))
	}
	turns, err := claudesession.ReadHistoryTurns(filePath, sessionHasInFlightSubmission(sess))
	if err != nil {
		return nil, nil, nil, err
	}
	thread := &codexrpc.ThreadReadThread{ID: strings.TrimSpace(sess.ActiveThreadID)}
	if meta != nil {
		if title := strings.TrimSpace(meta.Title); title != "" {
			thread.Name = &title
		}
		thread.Preview = strings.TrimSpace(meta.Preview)
		thread.Cwd = strings.TrimSpace(meta.Cwd)
	}
	if thread.Name == nil {
		if name := strings.TrimSpace(sess.ActiveThreadName); name != "" {
			thread.Name = &name
		}
	}
	if strings.TrimSpace(thread.Preview) == "" {
		thread.Preview = strings.TrimSpace(sess.ActiveThreadPreview)
	}
	return sess, thread, turns, nil
}

