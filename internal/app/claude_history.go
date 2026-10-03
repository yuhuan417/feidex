package app

import (
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	claudesession "feidex/internal/adapter/backend/claude/catalog"
	"feidex/internal/adapter/feishu/claudesupport"
	history "feidex/internal/adapter/feishu/history"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	appruntime "feidex/internal/runtime"
)

// ---------------------------------------------------------------------------
// History service type and constructor
// ---------------------------------------------------------------------------

func newClaudeHistoryService(a *App) *claudesupport.HistoryService {
	return &claudesupport.HistoryService{
		FetchClaudeSessionTurns: func(sessionKey string) (*conversation.Session, *codexrpc.ThreadReadThread, []appruntime.ClaudeHistoryTurnSummary, error) {
			return fetchClaudeCurrentSessionTurns(a, sessionKey)
		},
		ThreadLabel:  appthreadmenu.SessionCurrentThreadLabel,
		MenuCardBody: menuCardBody,
		Renderer:     claudeHistoryCardRenderer{app: a},
		PageSize:     history.HistoryPageSize,
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
	return newClaudeHistoryService(a).RenderHistoryDetailCard(sessionKey, index, nil)
}

type claudeHistoryCardRenderer struct{ app *App }

func (r claudeHistoryCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}

// ---------------------------------------------------------------------------
// Fetch helper — stays in app/ since it uses app-internal aliases
// ---------------------------------------------------------------------------

func fetchClaudeCurrentSessionTurns(a *App, sessionKey string) (*conversation.Session, *codexrpc.ThreadReadThread, []appruntime.ClaudeHistoryTurnSummary, error) {
	if a == nil || a.store == nil {
		return nil, nil, nil, fmt.Errorf("store not initialized")
	}
	sess := a.State().Session(sessionKey)
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
	turns, err := claudesession.ReadHistoryTurns(filePath, conversation.HasInFlightSubmission(sess))
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
