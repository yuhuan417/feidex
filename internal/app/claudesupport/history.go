package claudesupport

import (
	"fmt"
	"strconv"
	"strings"

	appcards "feidex/internal/app/cards"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/app/apputil"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------- callback types for history dependencies ----------

// FetchClaudeSessionTurnsFunc fetches Claude session turns for a session key.
type FetchClaudeSessionTurnsFunc func(sessionKey string) (*state.Session, *codexrpc.ThreadReadThread, []appruntime.ClaudeHistoryTurnSummary, error)

// ThreadLabelFunc returns the display label for the active thread.
type ThreadLabelFunc func(sess *state.Session) string

// MenuCardBodyFunc formats a menu card body with breadcrumb navigation.
type MenuCardBodyFunc func(action, body string) string

// ---------- HistoryService ----------

// HistoryService manages Claude history operations with callbacks for
// app/ dependencies.
type HistoryService struct {
	FetchClaudeSessionTurns FetchClaudeSessionTurnsFunc
	ThreadLabel             ThreadLabelFunc
	MenuCardBody            MenuCardBodyFunc
	PageSize                int
}

// HistoryTurnIndexForOrdinal returns the turn index for the given ordinal.
func (s *HistoryService) HistoryTurnIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	_, _, turns, err := s.FetchClaudeSessionTurns(sessionKey)
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

// RenderHistoryCard renders the history list card for the given session and page.
func (s *HistoryService) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	sess, thread, turns, err := s.FetchClaudeSessionTurns(sessionKey)
	if err != nil {
		return nil, err
	}
	if page < 0 {
		page = 0
	}
	total := len(turns)
	start := page * s.PageSize
	if start >= total && total > 0 {
		page = (total - 1) / s.PageSize
		start = page * s.PageSize
	}
	end := start + s.PageSize
	if end > total {
		end = total
	}
	label := s.ThreadLabel(sess)
	if label == "-" {
		label = apputil.FirstNonEmpty(derefString(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前 session: " + label,
		"session: `" + thread.ID + "`",
		fmt.Sprintf("turn 数: `%d`", total),
	}
	if total == 0 {
		bodyLines = append(bodyLines, "", "这个 Claude session 暂无可展示的 turn 记录。")
	} else {
		bodyLines = append(bodyLines, fmt.Sprintf("当前页: `%d-%d / %d`", start+1, end, total))
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
		turnLabel := fmt.Sprintf("Turn #%d | %s | %s", turn.Ordinal, apputil.FirstNonEmpty(turn.Status, "-"), apputil.FirstNonEmpty(turn.Preview, "-"))
		if turn.IsCurrent {
			turnLabel = "当前 · " + turnLabel
			initialOption = strconv.Itoa(idx)
		}
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
			Text:  apputil.Truncate(turnLabel, 72),
			Value: strconv.Itoa(idx),
		})
	}
	if page > 0 {
		buttons = append(buttons, feishu.Button{
			Text: "上一页",
			Type: "default",
			Value: map[string]any{
				"action":      "history.page",
				"session_key": sessionKey,
				"page":        page - 1,
			},
		})
	}
	if end < total {
		buttons = append(buttons, feishu.Button{
			Text: "下一页",
			Type: "default",
			Value: map[string]any{
				"action":      "history.page",
				"session_key": sessionKey,
				"page":        page + 1,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.tools",
			"session_key": sessionKey,
		},
	})
	card := appcards.NewMarkdownBodyCard("历史记录", "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.MenuCardBody("menu.history", strings.Join(bodyLines, "\n"))})
	if len(selectOptions) > 0 {
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
			"history_detail_select",
			"选择要查看的 turn",
			map[string]any{"action": "history.detail.select", "session_key": sessionKey},
			selectOptions,
			initialOption,
		))
	}
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement(buttons))
	return card, nil
}

// RenderHistoryDetailCard renders the history detail card for a specific turn
// using the provided SimpleStatusCard callback for card construction.
func (s *HistoryService) RenderHistoryDetailCard(sessionKey string, index int, simpleStatusCard SimpleStatusCardFunc) (map[string]any, error) {
	sess, thread, turns, err := s.FetchClaudeSessionTurns(sessionKey)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(turns) {
		return nil, fmt.Errorf("history turn index out of range")
	}
	turn := turns[index]
	label := s.ThreadLabel(sess)
	if label == "-" {
		label = apputil.FirstNonEmpty(derefString(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前 session: " + label,
		"session: `" + thread.ID + "`",
		fmt.Sprintf("Turn #%d", turn.Ordinal),
		"turn_id: `" + apputil.FirstNonEmpty(turn.TurnID, fmt.Sprintf("claude-turn-%d", turn.Ordinal)) + "`",
		"状态: `" + apputil.FirstNonEmpty(turn.Status, "-") + "`",
		fmt.Sprintf("记录数: `%d`", len(turn.Records)),
		"",
		"原始记录：",
	}
	if len(turn.Records) == 0 {
		bodyLines = append(bodyLines, "-")
	} else {
		for idx, record := range turn.Records {
			meta := []string{"`" + apputil.FirstNonEmpty(record.EntryType, "-") + "`"}
			if record.Timestamp != "" {
				meta = append(meta, "`"+record.Timestamp+"`")
			}
			bodyLines = append(bodyLines, fmt.Sprintf("%d. %s", idx+1, strings.Join(meta, " · ")))
			if record.PromptID != "" {
				bodyLines = append(bodyLines, "prompt_id: `"+record.PromptID+"`")
			}
			if record.MessageID != "" {
				bodyLines = append(bodyLines, "message_id: `"+record.MessageID+"`")
			}
			if record.StopReason != "" {
				bodyLines = append(bodyLines, "stop_reason: `"+record.StopReason+"`")
			}
			if len(record.Details) == 0 {
				bodyLines = append(bodyLines, "-")
				continue
			}
			for _, line := range record.Details {
				bodyLines = append(bodyLines, apputil.Truncate(line, 600))
			}
		}
	}
	buttons := make([]feishu.Button, 0, 3)
	if index > 0 {
		buttons = append(buttons, feishu.Button{
			Text: "更新一条",
			Type: "default",
			Value: map[string]any{
				"action":      "history.detail",
				"session_key": sessionKey,
				"index":       index - 1,
			},
		})
	}
	if index+1 < len(turns) {
		buttons = append(buttons, feishu.Button{
			Text: "更旧一条",
			Type: "default",
			Value: map[string]any{
				"action":      "history.detail",
				"session_key": sessionKey,
				"index":       index + 1,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "history.page",
			"session_key": sessionKey,
			"page":        index / s.PageSize,
		},
	})
	return simpleStatusCard("Turn 详情", "blue", s.MenuCardBody("history.detail", strings.Join(bodyLines, "\n")), buttons), nil
}

// derefString dereferences a string pointer, returning "" if nil.
func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
