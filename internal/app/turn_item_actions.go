package app

import (
	"encoding/json"
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) completeTurnItemToggle(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID, _ := action.ActionValue["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		if parsedID, _, ok := parseTurnItemToggleName(action.Name); ok {
			requestID = parsedID
		}
	}
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != "turn_item_card" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "详情卡已失效"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限操作这张卡片"}}, nil
	}
	var payload turnItemCardPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "详情卡数据损坏"}}, nil
	}
	expanded, _ := action.ActionValue["expanded"].(bool)
	if !expanded {
		if _, parsedExpanded, ok := parseTurnItemToggleName(action.Name); ok {
			expanded = parsedExpanded
		}
	}
	sub := appState.submission(payload.SubmissionID)
	includeActions := false
	if sess := appState.session(payload.SessionKey); sess != nil && sess.ActiveTurnID == payload.TurnID {
		includeActions = true
	}
	card := a.renderTurnItemCard(sub, payload, !expanded, includeActions, requestID)
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func parseTurnItemToggleName(name string) (requestID string, expanded bool, ok bool) {
	const prefix = "turn.item.toggle:"
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, prefix) {
		return "", false, false
	}
	parts := strings.Split(strings.TrimPrefix(name, prefix), ":")
	if len(parts) != 2 {
		return "", false, false
	}
	requestID = strings.TrimSpace(parts[0])
	state := strings.TrimSpace(parts[1])
	switch state {
	case "expanded":
		return requestID, true, requestID != ""
	case "collapsed":
		return requestID, false, requestID != ""
	default:
		return "", false, false
	}
}
