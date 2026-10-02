// Package historycmd provides the /history command service extracted from the
// app god package. It handles history listing, detail views, and Codex thread
// history card rendering.
package history

import (
	"context"
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"fmt"
	"strconv"
	"strings"
	"time"

	history "feidex/internal/adapter/backend/codex/history"
	"feidex/internal/adapter/feishu/cardactions"
	appcards "feidex/internal/adapter/feishu/cards"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
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

// StateProvider narrows app state access to the session lookup used by
// the history service.
type StateProvider interface {
	Session(key string) *conversation.Session
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

type FeishuClient interface {
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}
type Dependencies struct {
	Context       func() context.Context
	Feishu        FeishuClient
	State         StateProvider
	Codex         func() (CodexClient, error)
	SessionKey    func(*feishu.InboundMessage) string
	ReplyInThread func(string) bool
	MenuBody      func(string, string) string
	ThreadLabel   func(*conversation.Session) string
	HistoryIndex  func(string, int) (int, error)
	RenderHistory func(string, int) (map[string]any, error)
	RenderDetail  func(string, int) (map[string]any, error)
}

// ---------------------------------------------------------------------------
// Service — manages /history command actions
// ---------------------------------------------------------------------------

// Service manages history command actions for a single app instance.
type Service struct{ deps Dependencies }

// NewService creates a new history service bound to the given app.
func NewService(deps Dependencies) Service { return Service{deps: deps} }

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
		sessionKey := s.deps.SessionKey(msg)
		index, err := s.deps.HistoryIndex(sessionKey, ordinal)
		if err != nil {
			return err
		}
		card, err := s.deps.RenderDetail(sessionKey, index)
		if err != nil {
			return err
		}
		_, err = s.deps.Feishu.ReplyCard(s.context(), msg.MessageID, card, s.deps.ReplyInThread(msg.ChatType))
		return err
	}
	sessionKey := s.deps.SessionKey(msg)
	card, err := s.deps.RenderHistory(sessionKey, 0)
	if err != nil {
		return err
	}
	_, err = s.deps.Feishu.ReplyCard(s.context(), msg.MessageID, card, s.deps.ReplyInThread(msg.ChatType))
	return err
}

// ---------------------------------------------------------------------------
// Delegation to conversation backend
// ---------------------------------------------------------------------------

// HistoryIndexForOrdinal returns the turn index for the given ordinal.
func (s Service) HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return s.deps.HistoryIndex(sessionKey, ordinal)
}

// RenderHistoryCard renders the history list card for the given session and page.
func (s Service) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return s.deps.RenderHistory(sessionKey, page)
}

// RenderHistoryDetailCard renders the history detail card for the given session
// and turn index.
func (s Service) RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return s.deps.RenderDetail(sessionKey, index)
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
	label := s.deps.ThreadLabel(sess)
	if label == "-" {
		label = textutil.FirstNonEmpty(history.StringPtrValue(thread.Name), thread.Preview, thread.ID)
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
		turnLabel := fmt.Sprintf("Turn #%d | %s | %s", turn.Ordinal, textutil.FirstNonEmpty(turn.Status, "-"), textutil.FirstNonEmpty(turn.InputPreview, "-"))
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
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.tools", SessionKey: sessionKey}.Map(),
	})
	card := appcards.NewMarkdownBodyCard("历史记录", "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.deps.MenuBody("menu.history", strings.Join(bodyLines, "\n"))})
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
	label := s.deps.ThreadLabel(sess)
	if label == "-" {
		label = textutil.FirstNonEmpty(history.StringPtrValue(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前线程: " + label,
		"thread: `" + thread.ID + "`",
		fmt.Sprintf("Turn #%d", turn.Ordinal),
		"turn_id: `" + turn.TurnID + "`",
		"状态: `" + textutil.FirstNonEmpty(turn.Status, "-") + "`",
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
			bodyLines = append(bodyLines, fmt.Sprintf("%d. %s", i+1, textutil.Truncate(output, 600)))
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
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.HistoryPageActionValue{SessionKey: sessionKey, Page: index / HistoryPageSize}.Map(),
	})
	return s.deps.Feishu.SimpleStatusCard("Turn 详情", "blue", s.deps.MenuBody("history.detail", strings.Join(bodyLines, "\n")), buttons), nil
}

// ---------------------------------------------------------------------------
// Codex thread history fetching
// ---------------------------------------------------------------------------

// FetchCurrentThreadHistory fetches the current thread history from the Codex
// backend. Returns the session, thread, turn summaries, and any error.
func (s Service) FetchCurrentThreadHistory(sessionKey string) (*conversation.Session, *codexrpc.ThreadReadThread, []history.TurnSummary, error) {
	store := s.deps.State
	if store == nil {
		return nil, nil, nil, fmt.Errorf("store not initialized")
	}
	sess := s.deps.State.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, nil, nil, fmt.Errorf("当前没有活动线程")
	}
	ctx, cancel := context.WithTimeout(s.context(), 20*time.Second)
	defer cancel()
	var result codexrpc.ThreadReadResult
	client, err := s.deps.Codex()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := client.Call(ctx, "thread/read", map[string]any{
		"threadId":     strings.TrimSpace(sess.ActiveThreadID),
		"includeTurns": true,
	}, &result); err != nil {
		return nil, nil, nil, err
	}
	turns := history.SummarizeThreadHistory(result.Thread.Turns, sess.ActiveTurnID)
	return sess, &result.Thread, turns, nil
}

func (s Service) context() context.Context {
	if s.deps.Context != nil {
		return s.deps.Context()
	}
	return context.Background()
}
