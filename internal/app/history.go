package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const historyPageSize = 50
const historyCommandUsage = "/history | /history detail TURN_NUMBER"

type historyTurnSummary struct {
	Ordinal      int
	TurnID       string
	Status       string
	Inputs       []string
	Outputs      []string
	ErrorText    string
	IsCurrent    bool
	InputPreview string
}

func (a *App) commandHistory(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		if len(args) != 2 || strings.TrimSpace(args[0]) != "detail" {
			return fmt.Errorf("usage: %s", historyCommandUsage)
		}
		ordinal, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil || ordinal <= 0 {
			return fmt.Errorf("usage: %s", historyCommandUsage)
		}
		index, err := a.historyIndexForOrdinal(a.makeSessionKey(msg), ordinal)
		if err != nil {
			return err
		}
		card, err := a.renderHistoryDetailCard(a.makeSessionKey(msg), index)
		if err != nil {
			return err
		}
		_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
		return err
	}
	card, err := a.renderHistoryCard(a.makeSessionKey(msg), 0)
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) historyIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	_, _, turns, err := a.fetchCurrentThreadHistory(sessionKey)
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

func (a *App) renderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	sess, thread, turns, err := a.fetchCurrentThreadHistory(sessionKey)
	if err != nil {
		return nil, err
	}
	if page < 0 {
		page = 0
	}
	total := len(turns)
	start := page * historyPageSize
	if start >= total && total > 0 {
		page = (total - 1) / historyPageSize
		start = page * historyPageSize
	}
	end := start + historyPageSize
	if end > total {
		end = total
	}
	label := currentThreadLabel(sess)
	if label == "-" {
		label = firstNonEmpty(stringPtrValue(thread.Name), thread.Preview, thread.ID)
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
	selectOptions := make([]selectStaticOption, 0, end-start)
	initialOption := ""
	for idx := start; idx < end; idx++ {
		turn := turns[idx]
		label := fmt.Sprintf("Turn #%d | %s | %s", turn.Ordinal, firstNonEmpty(turn.Status, "-"), firstNonEmpty(turn.InputPreview, "-"))
		if turn.IsCurrent {
			label = "当前 · " + label
			initialOption = strconv.Itoa(idx)
		}
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  label,
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
	card := newMarkdownBodyCard("历史记录", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.history", strings.Join(bodyLines, "\n"))})
	if len(selectOptions) > 0 {
		appendMarkdownBodyCardElement(card, buildSelectStaticElement(
			"history_detail_select",
			"选择要查看的 turn",
			map[string]any{"action": "history.detail.select", "session_key": sessionKey},
			selectOptions,
			initialOption,
		))
	}
	appendMarkdownBodyCardElement(card, buildMarkdownBodyCardActionElement(buttons))
	return card, nil
}

func (a *App) renderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	sess, thread, turns, err := a.fetchCurrentThreadHistory(sessionKey)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(turns) {
		return nil, fmt.Errorf("history turn index out of range")
	}
	turn := turns[index]
	label := currentThreadLabel(sess)
	if label == "-" {
		label = firstNonEmpty(stringPtrValue(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前线程: " + label,
		"thread: `" + thread.ID + "`",
		fmt.Sprintf("Turn #%d", turn.Ordinal),
		"turn_id: `" + turn.TurnID + "`",
		"状态: `" + firstNonEmpty(turn.Status, "-") + "`",
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
			bodyLines = append(bodyLines, fmt.Sprintf("%d. %s", i+1, truncate(output, 600)))
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
			"page":        index / historyPageSize,
		},
	})
	return a.feishu.SimpleStatusCard("Turn 详情", "blue", menuCardBody("history.detail", strings.Join(bodyLines, "\n")), buttons), nil
}

func (a *App) fetchCurrentThreadHistory(sessionKey string) (*state.Session, *codexrpc.ThreadReadThread, []historyTurnSummary, error) {
	if a == nil || a.store == nil {
		return nil, nil, nil, fmt.Errorf("store not initialized")
	}
	sess := a.appState().session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, nil, nil, fmt.Errorf("当前没有活动线程")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var result codexrpc.ThreadReadResult
	if err := a.codex.Call(ctx, "thread/read", map[string]any{
		"threadId":     strings.TrimSpace(sess.ActiveThreadID),
		"includeTurns": true,
	}, &result); err != nil {
		return nil, nil, nil, err
	}
	turns := summarizeThreadHistory(result.Thread.Turns, sess.ActiveTurnID)
	return sess, &result.Thread, turns, nil
}

func summarizeThreadHistory(turns []codexrpc.ThreadReadTurn, currentTurnID string) []historyTurnSummary {
	summaries := make([]historyTurnSummary, 0, len(turns))
	for idx, turn := range turns {
		summary := historyTurnSummary{
			Ordinal:   idx + 1,
			TurnID:    strings.TrimSpace(turn.ID),
			Status:    strings.TrimSpace(turn.Status),
			IsCurrent: strings.TrimSpace(turn.ID) != "" && strings.TrimSpace(turn.ID) == strings.TrimSpace(currentTurnID),
		}
		if turn.Error != nil {
			summary.ErrorText = strings.TrimSpace(firstNonEmpty(turn.Error.Message, stringPtrValue(turn.Error.AdditionalDetails)))
		}
		for _, item := range turn.Items {
			switch strings.TrimSpace(item.Type) {
			case "userMessage":
				summary.Inputs = append(summary.Inputs, historyUserMessageInputs(item)...)
			case "agentMessage":
				if text := strings.TrimSpace(item.Text); text != "" {
					summary.Outputs = append(summary.Outputs, text)
				}
			}
		}
		summary.InputPreview = historyInputPreview(summary.Inputs)
		summaries = append(summaries, summary)
	}
	for i, j := 0, len(summaries)-1; i < j; i, j = i+1, j-1 {
		summaries[i], summaries[j] = summaries[j], summaries[i]
	}
	return summaries
}

func historyUserMessageInputs(item codexrpc.ThreadReadItem) []string {
	if len(item.Content) == 0 {
		return nil
	}
	var inputs []codexrpc.ThreadReadUserInput
	if err := json.Unmarshal(item.Content, &inputs); err != nil {
		return nil
	}
	rendered := make([]string, 0, len(inputs))
	for _, input := range inputs {
		switch strings.TrimSpace(input.Type) {
		case "text":
			if text := strings.TrimSpace(input.Text); text != "" {
				rendered = append(rendered, text)
			}
		case "image":
			rendered = append(rendered, "[image] "+firstNonEmpty(strings.TrimSpace(input.URL), "(no url)"))
		case "localImage":
			rendered = append(rendered, "[localImage] "+firstNonEmpty(strings.TrimSpace(input.Path), "(no path)"))
		case "skill":
			rendered = append(rendered, "[skill] "+firstNonEmpty(strings.TrimSpace(input.Name), strings.TrimSpace(input.Path), "(unknown skill)"))
		case "mention":
			rendered = append(rendered, "[mention] "+firstNonEmpty(strings.TrimSpace(input.Name), strings.TrimSpace(input.Path), "(unknown mention)"))
		}
	}
	return rendered
}

func historyInputPreview(inputs []string) string {
	if len(inputs) == 0 {
		return ""
	}
	if len(inputs) == 1 {
		return truncate(inputs[0], 72)
	}
	return truncate(inputs[0], 56) + fmt.Sprintf(" 等 %d 条", len(inputs))
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
