package history

import (
	"feidex/internal/adapter/feishu/cardactions"
	appcards "feidex/internal/adapter/feishu/cards"
	"feidex/internal/adapter/feishu/menuutil"
	historyapp "feidex/internal/application/history"
	"feidex/internal/domain/backend"
	"feidex/internal/feishu"
	"feidex/internal/textutil"
	"fmt"
	"strconv"
	"strings"
)

func RenderPage(sessionKey string, view historyapp.Page) map[string]any {
	if view.Backend == backend.BackendClaude {
		return renderClaudePage(sessionKey, view)
	}
	turns, page, start, end := view.Turns, view.Number, view.Start, view.End
	total := len(turns)
	label := view.Label
	bodyLines := []string{
		"当前线程: " + label,
		"thread: `" + view.ID + "`",
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
	elements := []map[string]any{}
	if len(selectOptions) > 0 {
		elements = append(elements, appcards.BuildSelectStaticElement(
			"history_detail_select",
			"选择要查看的 turn",
			cardactions.HistoryDetailSelectActionValue{SessionKey: sessionKey}.Map(),
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

func RenderDetail(sessionKey string, view historyapp.Detail) map[string]any {
	if view.Backend == backend.BackendClaude {
		return renderClaudeDetail(sessionKey, view)
	}
	turns, index, label := view.Turns, view.Index, view.Label
	turn := turns[index]
	bodyLines := []string{
		"当前线程: " + label,
		"thread: `" + view.ID + "`",
		fmt.Sprintf("Turn #%d", turn.Ordinal),
		"turn_id: `" + turn.ID + "`",
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
	return menuutil.PageCard{
		Node: "history.detail", SessionKey: sessionKey, Title: "Turn 详情", Color: "blue",
		Body:       strings.Join(bodyLines, "\n"),
		Buttons:    buttons,
		BackParams: map[string]any{"page": index / HistoryPageSize},
	}.Render()
}
