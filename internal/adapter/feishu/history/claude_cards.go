package history

import (
	appcards "feidex/internal/adapter/feishu/cards"
	"feidex/internal/adapter/feishu/menuutil"
	historyapp "feidex/internal/application/history"
	"feidex/internal/feishu"
	"feidex/internal/textutil"
	"fmt"
	"strconv"
	"strings"
)

func renderClaudePage(sessionKey string, view historyapp.Page) map[string]any {
	turns, page, start, end := view.Turns, view.Number, view.Start, view.End
	total := len(turns)
	label := view.Label
	bodyLines := []string{
		"当前 session: " + label,
		"session: `" + view.ID + "`",
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
		turnLabel := fmt.Sprintf("Turn #%d | %s | %s", turn.Ordinal, textutil.FirstNonEmpty(turn.Status, "-"), textutil.FirstNonEmpty(turn.InputPreview, "-"))
		if turn.IsCurrent {
			turnLabel = "当前 · " + turnLabel
			initialOption = strconv.Itoa(idx)
		}
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
			Text:  textutil.Truncate(turnLabel, 72),
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
	elements := []map[string]any{}
	if len(selectOptions) > 0 {
		elements = append(elements, appcards.BuildSelectStaticElement(
			"history_detail_select",
			"选择要查看的 turn",
			map[string]any{"action": "history.detail.select", "session_key": sessionKey},
			selectOptions,
			initialOption,
		))
	}
	return menuutil.MarkdownPageCard{
		Node: "menu.history", SessionKey: sessionKey, Title: "历史记录", Color: "blue",
		Body:     strings.Join(bodyLines, "\n"),
		Elements: elements,
		Buttons:  buttons,
	}.Render()
}

func renderClaudeDetail(sessionKey string, view historyapp.Detail) map[string]any {
	turns, index, label := view.Turns, view.Index, view.Label
	turn := turns[index]
	bodyLines := []string{
		"当前 session: " + label,
		"session: `" + view.ID + "`",
		fmt.Sprintf("Turn #%d", turn.Ordinal),
		"turn_id: `" + textutil.FirstNonEmpty(turn.ID, fmt.Sprintf("claude-turn-%d", turn.Ordinal)) + "`",
		"状态: `" + textutil.FirstNonEmpty(turn.Status, "-") + "`",
		fmt.Sprintf("记录数: `%d`", len(turn.Records)),
		"",
		"原始记录：",
	}
	if len(turn.Records) == 0 {
		bodyLines = append(bodyLines, "-")
	} else {
		for idx, record := range turn.Records {
			meta := []string{"`" + textutil.FirstNonEmpty(record.EntryType, "-") + "`"}
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
				bodyLines = append(bodyLines, textutil.Truncate(line, 600))
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
	return menuutil.PageCard{
		Node: "history.detail", SessionKey: sessionKey, Title: "Turn 详情", Color: "blue",
		Body:       strings.Join(bodyLines, "\n"),
		Buttons:    buttons,
		BackParams: map[string]any{"page": index / HistoryPageSize},
	}.Render()
}
