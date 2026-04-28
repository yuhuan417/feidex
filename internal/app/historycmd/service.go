// Package historycmd provides the /history command service extracted from the
// app god package. It handles history listing, detail views, and Codex thread
// history card rendering.
package historycmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appcore "feidex/internal/app/appcore"
	apphistory "feidex/internal/app/apphistory"
	apputil "feidex/internal/app/apputil"
	"feidex/internal/app/cardactions"
	appcards "feidex/internal/app/cards"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// HistoryPageSize is the number of turns displayed per page.
	HistoryPageSize = 50
	// HistoryCommandUsage is the usage string for the /history command.
	HistoryCommandUsage = "/history | /history detail TURN_NUMBER"
)

// ---------------------------------------------------------------------------
// Interfaces — what the service needs from the host application
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the session lookup used by
// the history service.
type AppStateProvider interface {
	Session(key string) *state.Session
}

// ConversationBackendProvider narrows conversation backend access to the
// methods used by the history service for delegation.
type ConversationBackendProvider interface {
	HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error)
	RenderHistoryCard(sessionKey string, page int) (map[string]any, error)
	RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error)
}

// CodexClient is the narrow interface for the Codex RPC client used by the
// history service.
type CodexClient interface {
	Call(ctx context.Context, method string, params any, out any) error
}

// App defines the interface the history service requires from the host
// application. It embeds appcore.AppConfig so that appcore helpers like
// MakeSessionKey, ReplyInThreadEnabled, etc. can be called directly.
type App interface {
	appcore.AppConfig

	// HistoryFeishu returns the Feishu bot client.
	HistoryFeishu() appcore.FeishuClient
	// HistoryAppState returns the narrowed app state provider.
	HistoryAppState() AppStateProvider
	// HistoryConversationBackend returns the narrowed conversation backend
	// provider for delegation.
	HistoryConversationBackend() ConversationBackendProvider
	// HistoryCodexClient returns the current Codex RPC client.
	HistoryCodexClient() (CodexClient, error)
	// HistoryMakeSessionKey builds a session key from an inbound message.
	HistoryMakeSessionKey(msg *feishu.InboundMessage) string
	// HistoryReplyInThreadEnabled reports whether reply-in-thread is enabled
	// for the given chat type.
	HistoryReplyInThreadEnabled(chatType string) bool
	// HistoryMenuCardBody formats a menu card body with breadcrumb navigation.
	HistoryMenuCardBody(action, body string) string
	// HistoryCurrentThreadLabel returns the display label for the active thread.
	HistoryCurrentThreadLabel(sess *state.Session) string
}

// ---------------------------------------------------------------------------
// Service — manages /history command actions
// ---------------------------------------------------------------------------

// Service manages history command actions for a single app instance.
type Service struct {
	app App
}

// NewService creates a new history service bound to the given app.
func NewService(app App) Service {
	return Service{app: app}
}

// ---------------------------------------------------------------------------
// Command handling
// ---------------------------------------------------------------------------

// CommandHistory handles the /history command with optional sub-commands.
func (s Service) CommandHistory(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		if len(args) != 2 || strings.TrimSpace(args[0]) != "detail" {
			return fmt.Errorf("usage: %s", HistoryCommandUsage)
		}
		ordinal, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil || ordinal <= 0 {
			return fmt.Errorf("usage: %s", HistoryCommandUsage)
		}
		sessionKey := s.app.HistoryMakeSessionKey(msg)
		index, err := s.HistoryIndexForOrdinal(sessionKey, ordinal)
		if err != nil {
			return err
		}
		card, err := s.RenderHistoryDetailCard(sessionKey, index)
		if err != nil {
			return err
		}
		_, err = s.app.HistoryFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.HistoryReplyInThreadEnabled(msg.ChatType))
		return err
	}
	sessionKey := s.app.HistoryMakeSessionKey(msg)
	card, err := s.RenderHistoryCard(sessionKey, 0)
	if err != nil {
		return err
	}
	_, err = s.app.HistoryFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.HistoryReplyInThreadEnabled(msg.ChatType))
	return err
}

// ---------------------------------------------------------------------------
// Delegation to conversation backend
// ---------------------------------------------------------------------------

// HistoryIndexForOrdinal returns the turn index for the given ordinal.
func (s Service) HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return s.app.HistoryConversationBackend().HistoryIndexForOrdinal(sessionKey, ordinal)
}

// RenderHistoryCard renders the history list card for the given session and page.
func (s Service) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return s.app.HistoryConversationBackend().RenderHistoryCard(sessionKey, page)
}

// RenderHistoryDetailCard renders the history detail card for the given session
// and turn index.
func (s Service) RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return s.app.HistoryConversationBackend().RenderHistoryDetailCard(sessionKey, index)
}

// ---------------------------------------------------------------------------
// Codex-specific implementations
// ---------------------------------------------------------------------------

// CodexHistoryIndexForOrdinal finds the turn index for the given ordinal using
// the Codex thread history.
func (s Service) CodexHistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	_, _, turns, err := s.FetchCurrentThreadHistory(sessionKey)
	if err != nil {
		return 0, err
	}
	for idx, turn := range turns {
		if turn.Ordinal == ordinal {
			return idx, nil
		}
	}
	return 0, fmt.Errorf("Turn #%d 不存在", ordinal)
}

// RenderCodexHistoryCard renders the Codex history list card with pagination.
func (s Service) RenderCodexHistoryCard(sessionKey string, page int) (map[string]any, error) {
	sess, thread, turns, err := s.FetchCurrentThreadHistory(sessionKey)
	if err != nil {
		return nil, err
	}
	if page < 0 {
		page = 0
	}
	total := len(turns)
	start := page * HistoryPageSize
	if start >= total && total > 0 {
		page = (total - 1) / HistoryPageSize
		start = page * HistoryPageSize
	}
	end := start + HistoryPageSize
	if end > total {
		end = total
	}
	label := s.app.HistoryCurrentThreadLabel(sess)
	if label == "-" {
		label = apputil.FirstNonEmpty(apphistory.StringPtrValue(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前线程: " + label,
		"thread: `" + thread.ID + "`",
		fmt.Sprintf("turn 数: `%d`", total),
	}
	if total == 0 {
		bodyLines = append(bodyLines, "", "这个 thread 暂无可展示的 turn 记录。")
	} else {
		bodyLines = append(bodyLines,
			fmt.Sprintf("当前页: `%d-%d / %d`", start+1, end, total),
		)
		for _, turn := range turns {
			if turn.IsCurrent {
				bodyLines = append(bodyLines, fmt.Sprintf("当前 turn: `Turn #%d`", turn.Ordinal))
				break
			}
		}
		bodyLines = append(bodyLines, "", "在线下拉菜单中选择要查看的 turn。")
	}

	buttons := make([]feishu.Button, 0, 3)
	selectOptions := make([]appcards.SelectStaticOption, 0, end-start)
	initialOption := ""
	for idx := start; idx < end; idx++ {
		turn := turns[idx]
		turnLabel := fmt.Sprintf("Turn #%d | %s | %s", turn.Ordinal, apputil.FirstNonEmpty(turn.Status, "-"), apputil.FirstNonEmpty(turn.InputPreview, "-"))
		if turn.IsCurrent {
			turnLabel = "当前 · " + turnLabel
			initialOption = strconv.Itoa(idx)
		}
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
			Text:  turnLabel,
			Value: strconv.Itoa(idx),
		})
	}
	if page > 0 {
		buttons = append(buttons, feishu.Button{
			Text:  "上一页",
			Type:  "default",
			Value: cardactions.HistoryPageActionValue{SessionKey: sessionKey, Page: page - 1}.Map(),
		})
	}
	if end < total {
		buttons = append(buttons, feishu.Button{
			Text:  "下一页",
			Type:  "default",
			Value: cardactions.HistoryPageActionValue{SessionKey: sessionKey, Page: page + 1}.Map(),
		})
	}
	buttons = append(buttons, feishu.Button{
		Text:  "返回上一级",
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.tools", SessionKey: sessionKey}.Map(),
	})
	card := appcards.NewMarkdownBodyCard("历史记录", "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.app.HistoryMenuCardBody("menu.history", strings.Join(bodyLines, "\n"))})
	if len(selectOptions) > 0 {
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
			"history_detail_select",
			"选择要查看的 turn",
			cardactions.HistoryDetailSelectActionValue{SessionKey: sessionKey}.Map(),
			selectOptions,
			initialOption,
		))
	}
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement(buttons))
	return card, nil
}

// RenderCodexHistoryDetailCard renders the Codex history detail card for a
// specific turn.
func (s Service) RenderCodexHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	sess, thread, turns, err := s.FetchCurrentThreadHistory(sessionKey)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(turns) {
		return nil, fmt.Errorf("history turn index out of range")
	}
	turn := turns[index]
	label := s.app.HistoryCurrentThreadLabel(sess)
	if label == "-" {
		label = apputil.FirstNonEmpty(apphistory.StringPtrValue(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前线程: " + label,
		"thread: `" + thread.ID + "`",
		fmt.Sprintf("Turn #%d", turn.Ordinal),
		"turn_id: `" + turn.TurnID + "`",
		"状态: `" + apputil.FirstNonEmpty(turn.Status, "-") + "`",
	}
	if turn.ErrorText != "" {
		bodyLines = append(bodyLines, "错误: "+turn.ErrorText)
	}
	bodyLines = append(bodyLines, "")
	bodyLines = append(bodyLines, "输入：")
	if len(turn.Inputs) == 0 {
		bodyLines = append(bodyLines, "-")
	} else {
		for i, input := range turn.Inputs {
			bodyLines = append(bodyLines, fmt.Sprintf("%d. %s", i+1, input))
		}
	}
	bodyLines = append(bodyLines, "", "回复：")
	if len(turn.Outputs) == 0 {
		bodyLines = append(bodyLines, "-")
	} else {
		for i, output := range turn.Outputs {
			bodyLines = append(bodyLines, fmt.Sprintf("%d. %s", i+1, apputil.Truncate(output, 600)))
		}
	}
	buttons := make([]feishu.Button, 0, 3)
	if index > 0 {
		buttons = append(buttons, feishu.Button{
			Text:  "更新一条",
			Type:  "default",
			Value: cardactions.HistoryDetailActionValue{SessionKey: sessionKey, Index: index - 1}.Map(),
		})
	}
	if index+1 < len(turns) {
		buttons = append(buttons, feishu.Button{
			Text:  "更旧一条",
			Type:  "default",
			Value: cardactions.HistoryDetailActionValue{SessionKey: sessionKey, Index: index + 1}.Map(),
		})
	}
	buttons = append(buttons, feishu.Button{
		Text:  "返回上一级",
		Type:  "default",
		Value: cardactions.HistoryPageActionValue{SessionKey: sessionKey, Page: index / HistoryPageSize}.Map(),
	})
	return s.app.HistoryFeishu().SimpleStatusCard("Turn 详情", "blue", s.app.HistoryMenuCardBody("history.detail", strings.Join(bodyLines, "\n")), buttons), nil
}

// ---------------------------------------------------------------------------
// Codex thread history fetching
// ---------------------------------------------------------------------------

// FetchCurrentThreadHistory fetches the current thread history from the Codex
// backend. Returns the session, thread, turn summaries, and any error.
func (s Service) FetchCurrentThreadHistory(sessionKey string) (*state.Session, *codexrpc.ThreadReadThread, []apphistory.TurnSummary, error) {
	store := s.app.Store()
	if store == nil {
		return nil, nil, nil, fmt.Errorf("store not initialized")
	}
	sess := s.app.HistoryAppState().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, nil, nil, fmt.Errorf("当前没有活动线程")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var result codexrpc.ThreadReadResult
	client, err := s.app.HistoryCodexClient()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := client.Call(ctx, "thread/read", map[string]any{
		"threadId":     strings.TrimSpace(sess.ActiveThreadID),
		"includeTurns": true,
	}, &result); err != nil {
		return nil, nil, nil, err
	}
	turns := apphistory.SummarizeThreadHistory(result.Thread.Turns, sess.ActiveTurnID)
	return sess, &result.Thread, turns, nil
}
