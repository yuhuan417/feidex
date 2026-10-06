package goalcmd

import (
	"context"
	goalapp "feidex/internal/application/goal"
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"fmt"
	"strconv"
	"strings"

	appcards "feidex/internal/adapter/feishu/cards"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	CommandUsage      = "/goal | /goal <objective> | /goal pause | /goal resume | /goal clear | /goal edit"
	MaxObjectiveRunes = 4000
)

type StateProvider interface {
	Session(key string) *conversation.Session
	Sessions() []*conversation.Session
}

// Outbound is the semantic messaging capability used by goal commands. The
// composition root supplies the implementation; goal orchestration never
// reaches into the Feishu transport client directly.
type Outbound interface {
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	ReplyText(context.Context, string, string, bool) error
	SendCard(context.Context, string, map[string]any) (string, error)
}

type CardRenderer interface {
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}

// Dependencies is the explicit capability set consumed by goal commands.
type Dependencies struct {
	StateProvider StateProvider
	Outbound      Outbound
	CardRenderer  CardRenderer

	GoalTracker              *goalapp.Tracker
	GoalManagement           *goalapp.Management
	MakeSessionKeyFn         func(*feishu.InboundMessage) string
	ReplyInThreadEnabledFn   func(string) bool
	MenuCardBodyForSessionFn func(string, string, string) string
	ActionStringValueFn      func(*feishu.CardAction, string) string
	ActionSessionKeyFn       func(*feishu.CardAction) string
	CompleteMenuCommandFn    func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
	ContextFn                func() context.Context
}

func (d Dependencies) State() StateProvider   { return d.StateProvider }
func (d Dependencies) Messaging() Outbound    { return d.Outbound }
func (d Dependencies) Renderer() CardRenderer { return d.CardRenderer }

func (d Dependencies) Tracker() *goalapp.Tracker { return d.GoalTracker }
func (d Dependencies) MakeSessionKey(m *feishu.InboundMessage) string {
	if d.MakeSessionKeyFn == nil {
		return ""
	}
	return d.MakeSessionKeyFn(m)
}
func (d Dependencies) ReplyInThreadEnabled(v string) bool {
	return d.ReplyInThreadEnabledFn != nil && d.ReplyInThreadEnabledFn(v)
}
func (d Dependencies) MenuCardBodyForSession(s, a, b string) string {
	if d.MenuCardBodyForSessionFn == nil {
		return b
	}
	return d.MenuCardBodyForSessionFn(s, a, b)
}
func (d Dependencies) ActionStringValue(a *feishu.CardAction, k string) string {
	if d.ActionStringValueFn == nil {
		return ""
	}
	return d.ActionStringValueFn(a, k)
}
func (d Dependencies) ActionSessionKey(a *feishu.CardAction) string {
	if d.ActionSessionKeyFn == nil {
		return ""
	}
	return d.ActionSessionKeyFn(a)
}
func (d Dependencies) CompleteMenuCommand(a *feishu.CardAction, s, r, f string) (*callback.CardActionTriggerResponse, error) {
	if d.CompleteMenuCommandFn == nil {
		return nil, fmt.Errorf("menu command unavailable")
	}
	return d.CompleteMenuCommandFn(a, s, r, f)
}

func (d Dependencies) Context() context.Context {
	if d.ContextFn != nil {
		if c := d.ContextFn(); c != nil {
			return c
		}
	}
	return context.Background()
}

type Service struct {
	app Dependencies
}

func NewService(a Dependencies) Service {
	return Service{app: a}
}

func (s Service) CommandGoal(msg *feishu.InboundMessage, raw string, args []string) error {
	a := s.app
	if a.StateProvider == nil || msg == nil {
		return nil
	}
	sessionKey := a.MakeSessionKey(msg)
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动 Codex thread，无法使用 /goal；先发送一条普通消息或恢复一个 thread")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	tail := goalCommandTail(raw, args)
	lowerTail := strings.ToLower(strings.TrimSpace(tail))
	switch {
	case lowerTail == "" || lowerTail == "status":
		goal, err := s.threadGoalGet(threadID)
		if err != nil {
			return fmt.Errorf("%s", goalFriendlyError("读取", err))
		}
		return s.replyGoalCard(msg, sessionKey, threadID, goal)
	case goalIsSingleControlWord(tail, "pause"):
		goal, err := s.threadGoalSetStatus(threadID, conversation.ThreadGoalStatusPaused)
		if err != nil {
			return fmt.Errorf("%s", goalFriendlyError("更新", err))
		}
		return s.replyGoalCard(msg, sessionKey, threadID, goal)
	case goalIsSingleControlWord(tail, "resume"):
		goal, err := s.threadGoalSetStatus(threadID, conversation.ThreadGoalStatusActive)
		if err != nil {
			return fmt.Errorf("%s", goalFriendlyError("更新", err))
		}
		return s.replyGoalCard(msg, sessionKey, threadID, goal)
	case goalIsSingleControlWord(tail, "clear"):
		cleared, err := s.threadGoalClear(threadID)
		if err != nil {
			return fmt.Errorf("%s", goalFriendlyError("清除", err))
		}
		return s.replyGoalClearedCard(msg, sessionKey, threadID, cleared)
	case goalIsSingleControlWord(tail, "edit"):
		goal, err := s.threadGoalGet(threadID)
		if err != nil {
			return fmt.Errorf("%s", goalFriendlyError("读取", err))
		}
		if goal == nil {
			return s.replyGoalCard(msg, sessionKey, threadID, nil)
		}
		card := s.renderGoalEditCard(sessionKey, threadID, *goal)
		_, err = a.Outbound.ReplyCard(s.app.Context(), msg.MessageID, card, a.ReplyInThreadEnabled(msg.ChatType))
		s.recordContext(sessionKey, threadID, msg)
		return err
	default:
		objective := strings.TrimSpace(tail)
		if err := validateGoalObjective(objective); err != nil {
			return err
		}
		result, err := s.app.GoalManagement.ProposeObjective(threadID, objective)
		if err != nil {
			return fmt.Errorf("%s", goalFriendlyError("设置", err))
		}
		if result.NeedsConfirmation {
			card := s.renderGoalReplaceConfirmCard(sessionKey, threadID, *result.Goal, objective)
			_, err := a.Outbound.ReplyCard(s.app.Context(), msg.MessageID, card, a.ReplyInThreadEnabled(msg.ChatType))
			s.recordContext(sessionKey, threadID, msg)
			return err
		}
		return s.replyGoalSetText(msg, sessionKey, threadID)
	}
}

func (s Service) replyGoalSetText(msg *feishu.InboundMessage, sessionKey, threadID string) error {
	if s.app.StateProvider == nil || s.app.Outbound == nil || msg == nil {
		return nil
	}
	s.recordContext(sessionKey, threadID, msg)
	return s.app.Outbound.ReplyText(s.app.Context(), msg.MessageID, "已设置 goal。", s.app.ReplyInThreadEnabled(msg.ChatType))
}

func goalCommandTail(raw string, args []string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "/goal") {
		return strings.TrimSpace(raw[len("/goal"):])
	}
	return strings.TrimSpace(strings.Join(args, " "))
}

func goalIsSingleControlWord(tail, word string) bool {
	fields := strings.Fields(strings.TrimSpace(tail))
	return len(fields) == 1 && strings.EqualFold(fields[0], word)
}

func validateGoalObjective(objective string) error { return goalapp.ValidateObjective(objective) }

func editedGoalStatus(status conversation.ThreadGoalStatus) conversation.ThreadGoalStatus {
	return goalapp.EditedStatus(status)
}

func (s Service) threadGoalGet(threadID string) (*conversation.ThreadGoal, error) {
	return s.app.GoalManagement.Get(threadID)
}

func (s Service) threadGoalSetStatus(threadID string, status conversation.ThreadGoalStatus) (*conversation.ThreadGoal, error) {
	return s.threadGoalSetObjective(threadID, "", status, nil)
}

func (s Service) threadGoalSetObjective(threadID, objective string, status conversation.ThreadGoalStatus, tokenBudget *int64) (*conversation.ThreadGoal, error) {
	return s.app.GoalManagement.Set(threadID, objective, status, tokenBudget)
}

func (s Service) threadGoalClear(threadID string) (bool, error) {
	return s.app.GoalManagement.Clear(threadID)
}

func goalFriendlyError(action string, err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "ephemeral thread does not support goals") ||
		strings.Contains(lower, "thread goals require a persisted thread"):
		return "Goals 需要已保存的 Codex session；当前 thread 是临时 thread。请先发送一条普通消息创建持久 thread 后再使用 /goal。"
	case strings.Contains(lower, "goals feature is disabled"):
		return "当前 Codex runtime 未启用 goals feature，无法使用 /goal。"
	case strings.Contains(lower, "no goal exists"):
		return "当前 thread 没有可更新的 goal。"
	case strings.Contains(lower, "thread not found"):
		return "当前 Codex thread 不存在或已失效，无法" + action + " goal。"
	default:
		return "无法" + action + " thread goal: " + text
	}
}

func (s Service) replyGoalCard(msg *feishu.InboundMessage, sessionKey, threadID string, goal *conversation.ThreadGoal) error {
	card := s.renderGoalCard(sessionKey, threadID, goal)
	_, err := s.app.Outbound.ReplyCard(s.app.Context(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	s.recordContext(sessionKey, threadID, msg)
	return err
}

func (s Service) replyGoalClearedCard(msg *feishu.InboundMessage, sessionKey, threadID string, cleared bool) error {
	body := "当前 thread 没有 goal。"
	color := "grey"
	title := "Goal"
	if cleared {
		body = "已清除当前 thread goal。"
		color = "green"
		title = "Goal cleared"
	}
	card := s.app.Renderer().SimpleStatusCard(title, color, s.app.MenuCardBodyForSession(sessionKey, "menu.goal", body), goalBackButtons(sessionKey))
	_, err := s.app.Outbound.ReplyCard(s.app.Context(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	s.recordContext(sessionKey, threadID, msg)
	return err
}

func (s Service) renderGoalCard(sessionKey, threadID string, goal *conversation.ThreadGoal) map[string]any {
	if goal == nil {
		return s.renderGoalCreateCard(sessionKey, threadID)
	}
	body := renderGoalBody(*goal)
	return s.app.Renderer().SimpleStatusCard("Goal "+goalStatusLabel(goal.Status), goalStatusColor(goal.Status), s.app.MenuCardBodyForSession(sessionKey, "menu.goal", body), goalButtons(sessionKey, threadID, goal.Status))
}

func (s Service) renderGoalSavedCard(goal *conversation.ThreadGoal) map[string]any {
	lines := []string{"已设置 goal。"}
	if goal != nil && strings.TrimSpace(goal.Objective) != "" {
		lines = append(lines, "objective: "+strings.TrimSpace(goal.Objective))
	}
	return s.app.Renderer().SimpleStatusCard("Goal set", "green", strings.Join(lines, "\n"), nil)
}

func renderGoalBody(goal conversation.ThreadGoal) string {
	lines := []string{
		"status: `" + goalStatusLabel(goal.Status) + "`",
		"objective: " + strings.TrimSpace(goal.Objective),
		"time used: `" + formatGoalElapsedSeconds(goal.TimeUsedSeconds) + "`",
		"tokens used: `" + formatGoalTokens(goal.TokensUsed) + "`",
	}
	if goal.TokenBudget != nil {
		lines = append(lines, "token budget: `"+formatGoalTokens(*goal.TokenBudget)+"`")
	}
	return strings.Join(lines, "\n")
}

func goalStatusLabel(status conversation.ThreadGoalStatus) string {
	switch status {
	case conversation.ThreadGoalStatusActive:
		return "active"
	case conversation.ThreadGoalStatusPaused:
		return "paused"
	case conversation.ThreadGoalStatusBlocked:
		return "blocked"
	case conversation.ThreadGoalStatusUsageLimited:
		return "usage limited"
	case conversation.ThreadGoalStatusBudgetLimited:
		return "limited by budget"
	case conversation.ThreadGoalStatusComplete:
		return "complete"
	default:
		return string(status)
	}
}

func goalStatusColor(status conversation.ThreadGoalStatus) string {
	switch status {
	case conversation.ThreadGoalStatusActive:
		return "green"
	case conversation.ThreadGoalStatusPaused,
		conversation.ThreadGoalStatusBlocked,
		conversation.ThreadGoalStatusUsageLimited,
		conversation.ThreadGoalStatusBudgetLimited:
		return "orange"
	case conversation.ThreadGoalStatusComplete:
		return "blue"
	default:
		return "blue"
	}
}

func formatGoalElapsedSeconds(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if hours >= 24 {
		days := hours / 24
		remainingHours := hours % 24
		return fmt.Sprintf("%dd %dh %dm", days, remainingHours, remainingMinutes)
	}
	if remainingMinutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, remainingMinutes)
}

func formatGoalTokens(tokens int64) string {
	sign := ""
	if tokens < 0 {
		sign = "-"
		tokens = -tokens
	}
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%s%.1fM", sign, float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%s%.1fK", sign, float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%s%d", sign, tokens)
	}
}

func goalBackButtons(sessionKey string) []feishu.Button {
	return []feishu.Button{{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Name:  "goal_back",
		Value: map[string]any{"action": "menu.tools", "session_key": sessionKey},
	}}
}

func goalButtons(sessionKey, threadID string, status conversation.ThreadGoalStatus) []feishu.Button {
	buttons := []feishu.Button{{
		Text:  "编辑",
		Type:  "default",
		Name:  "goal_edit",
		Value: map[string]any{"action": "goal.edit", "session_key": sessionKey, "thread_id": threadID},
	}}
	switch status {
	case conversation.ThreadGoalStatusActive:
		buttons = append(buttons, feishu.Button{
			Text:  "暂停",
			Type:  "default",
			Name:  "goal_pause",
			Value: map[string]any{"action": "goal.pause", "session_key": sessionKey, "thread_id": threadID},
		})
	case conversation.ThreadGoalStatusPaused,
		conversation.ThreadGoalStatusBlocked,
		conversation.ThreadGoalStatusUsageLimited:
		buttons = append(buttons, feishu.Button{
			Text:  "恢复",
			Type:  "primary",
			Name:  "goal_resume",
			Value: map[string]any{"action": "goal.resume", "session_key": sessionKey, "thread_id": threadID},
		})
	}
	buttons = append(buttons,
		feishu.Button{
			Text:  "清除",
			Type:  "danger",
			Name:  "goal_clear",
			Value: map[string]any{"action": "goal.clear", "session_key": sessionKey, "thread_id": threadID},
		},
		goalBackButtons(sessionKey)[0],
	)
	return buttons
}

func (s Service) renderGoalReplaceConfirmCard(sessionKey, threadID string, existing conversation.ThreadGoal, objective string) map[string]any {
	body := strings.Join([]string{
		"当前 thread 已有未完成 goal。",
		"",
		"当前 goal:",
		strings.TrimSpace(existing.Objective),
		"",
		"新 goal:",
		strings.TrimSpace(objective),
	}, "\n")
	return s.app.Renderer().SimpleStatusCard("Replace goal?", "orange", s.app.MenuCardBodyForSession(sessionKey, "menu.goal", body), []feishu.Button{
		{
			Text: "替换当前 goal",
			Type: "danger",
			Name: "goal_replace_confirm",
			Value: map[string]any{
				"action":      "goal.replace.confirm",
				"session_key": sessionKey,
				"thread_id":   threadID,
				"objective":   objective,
			},
		},
		{
			Text: "保留当前 goal",
			Type: "default",
			Name: "goal_replace_cancel",
			Value: map[string]any{
				"action":      "goal.replace.cancel",
				"session_key": sessionKey,
				"thread_id":   threadID,
			},
		},
	})
}

func (s Service) renderGoalEditCard(sessionKey, threadID string, goal conversation.ThreadGoal) map[string]any {
	status := editedGoalStatus(goal.Status)
	value := map[string]any{
		"action":      "goal.edit.submit",
		"session_key": sessionKey,
		"thread_id":   threadID,
		"status":      string(status),
	}
	if goal.TokenBudget != nil {
		value["token_budget"] = strconv.FormatInt(*goal.TokenBudget, 10)
	}
	return s.renderGoalObjectiveFormCard(goalObjectiveFormOptions{
		Title:            "Edit goal",
		Body:             "编辑当前 thread goal。",
		SessionKey:       sessionKey,
		DefaultObjective: strings.TrimSpace(goal.Objective),
		SubmitText:       "保存",
		CancelAction:     "menu.goal",
		SubmitValue:      value,
	})
}

type goalObjectiveFormOptions struct {
	Title            string
	Body             string
	SessionKey       string
	DefaultObjective string
	SubmitText       string
	CancelAction     string
	SubmitValue      map[string]any
}

func (s Service) renderGoalCreateCard(sessionKey, threadID string) map[string]any {
	return s.renderGoalObjectiveFormCard(goalObjectiveFormOptions{
		Title:        "Create goal",
		Body:         "当前 thread 没有 goal。输入 objective 创建 active goal。",
		SessionKey:   sessionKey,
		SubmitText:   "创建",
		CancelAction: "menu.tools",
		SubmitValue: map[string]any{
			"action":      "goal.edit.submit",
			"session_key": sessionKey,
			"thread_id":   threadID,
			"status":      string(conversation.ThreadGoalStatusActive),
		},
	})
}

func (s Service) renderGoalObjectiveFormCard(opts goalObjectiveFormOptions) map[string]any {
	card := appcards.NewMarkdownBodyCard(opts.Title, "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": s.app.MenuCardBodyForSession(opts.SessionKey, "menu.goal", opts.Body),
	})
	objectiveInput := map[string]any{
		"tag":         "input",
		"name":        "objective",
		"required":    true,
		"placeholder": map[string]any{"tag": "plain_text", "content": "Goal objective"},
	}
	if opts.DefaultObjective != "" {
		objectiveInput["default_value"] = opts.DefaultObjective
	}
	buttonRows := appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  firstNonEmpty(opts.SubmitText, "保存"),
			Type:  "primary",
			Name:  "goal_edit_submit",
			Value: opts.SubmitValue,
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "goal_edit_cancel",
			Value: map[string]any{"action": firstNonEmpty(opts.CancelAction, "menu.goal"), "session_key": opts.SessionKey},
		},
	})
	if len(buttonRows) > 0 {
		markFirstButtonAsSubmit(buttonRows[0])
	}
	form := map[string]any{
		"tag":                "form",
		"name":               "goal_edit_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           append([]map[string]any{objectiveInput}, buttonRows...),
	}
	appcards.AppendMarkdownBodyCardElement(card, form)
	return card
}

func markFirstButtonAsSubmit(row map[string]any) {
	columns, _ := row["columns"].([]map[string]any)
	if len(columns) == 0 {
		return
	}
	elements, _ := columns[0]["elements"].([]map[string]any)
	if len(elements) == 0 {
		return
	}
	elements[0]["form_action_type"] = "submit"
}

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func (s Service) CompleteMenuGoal(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.CompleteMenuCommand(action, sessionKey, "/goal", "menu.tools")
}

func (s Service) CompleteGoalStatusAction(action *feishu.CardAction, status conversation.ThreadGoalStatus) (*callback.CardActionTriggerResponse, error) {
	sessionKey, threadID, err := s.goalActionSessionThread(action)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	goal, err := s.threadGoalSetStatus(threadID, status)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: goalFriendlyError("更新", err)}}, nil
	}
	s.recordContextFromAction(action, sessionKey, threadID)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 goal"},
		Card:  rawCard(s.renderGoalCard(sessionKey, threadID, goal)),
	}, nil
}

func (s Service) CompleteGoalClearAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, threadID, err := s.goalActionSessionThread(action)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	cleared, err := s.threadGoalClear(threadID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: goalFriendlyError("清除", err)}}, nil
	}
	s.recordContextFromAction(action, sessionKey, threadID)
	body := "当前 thread 没有 goal。"
	color := "grey"
	title := "Goal"
	if cleared {
		body = "已清除当前 thread goal。"
		color = "green"
		title = "Goal cleared"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: goalClearedToast(cleared)},
		Card:  rawCard(s.app.Renderer().SimpleStatusCard(title, color, s.app.MenuCardBodyForSession(sessionKey, "menu.goal", body), goalBackButtons(sessionKey))),
	}, nil
}

func goalClearedToast(cleared bool) string {
	if cleared {
		return "已清除 goal"
	}
	return "当前没有 goal"
}

func (s Service) CompleteGoalEditAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, threadID, err := s.goalActionSessionThread(action)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	goal, err := s.threadGoalGet(threadID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: goalFriendlyError("读取", err)}}, nil
	}
	s.recordContextFromAction(action, sessionKey, threadID)
	if goal == nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前 thread 没有 goal"},
			Card:  rawCard(s.renderGoalCard(sessionKey, threadID, nil)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 goal 编辑"},
		Card:  rawCard(s.renderGoalEditCard(sessionKey, threadID, *goal)),
	}, nil
}

func (s Service) CompleteGoalReplaceConfirm(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, threadID, err := s.goalActionSessionThread(action)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	objective := s.app.ActionStringValue(action, "objective")
	if err := validateGoalObjective(objective); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	goal, err := s.app.GoalManagement.Replace(threadID, objective)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: goalFriendlyError("设置", err)}}, nil
	}
	s.recordContextFromAction(action, sessionKey, threadID)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已替换 goal"},
		Card:  rawCard(s.renderGoalSavedCard(goal)),
	}, nil
}

func (s Service) CompleteGoalReplaceCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, threadID, err := s.goalActionSessionThread(action)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	goal, err := s.threadGoalGet(threadID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: goalFriendlyError("读取", err)}}, nil
	}
	s.recordContextFromAction(action, sessionKey, threadID)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已保留当前 goal"},
		Card:  rawCard(s.renderGoalCard(sessionKey, threadID, goal)),
	}, nil
}

func (s Service) CompleteGoalEditSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, threadID, err := s.goalActionSessionThread(action)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	objective := strings.TrimSpace(goalFormStringValue(action, "objective"))
	if err := validateGoalObjective(objective); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	status := conversation.ThreadGoalStatus(s.app.ActionStringValue(action, "status"))
	if status == "" {
		status = conversation.ThreadGoalStatusActive
	}
	var tokenBudget *int64
	if rawBudget := s.app.ActionStringValue(action, "token_budget"); rawBudget != "" {
		value, err := strconv.ParseInt(rawBudget, 10, 64)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "goal token budget 参数损坏"}}, nil
		}
		tokenBudget = &value
	}
	goal, err := s.threadGoalSetObjective(threadID, objective, status, tokenBudget)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: goalFriendlyError("更新", err)}}, nil
	}
	s.recordContextFromAction(action, sessionKey, threadID)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 goal"},
		Card:  rawCard(s.renderGoalSavedCard(goal)),
	}, nil
}

func (s Service) goalActionSessionThread(action *feishu.CardAction) (string, string, error) {
	sessionKey := s.app.ActionSessionKey(action)
	threadID := s.app.ActionStringValue(action, "thread_id")
	if sessionKey == "" || threadID == "" {
		return "", "", fmt.Errorf("goal 操作参数缺失")
	}
	sess := s.app.State().Session(sessionKey)
	if sess == nil {
		return "", "", fmt.Errorf("当前 session 已失效")
	}
	if strings.TrimSpace(sess.ActiveThreadID) != threadID {
		return "", "", fmt.Errorf("当前 thread 已变化，请重新打开 /goal")
	}
	return sessionKey, threadID, nil
}

func goalFormStringValue(action *feishu.CardAction, key string) string {
	if action == nil || action.FormValue == nil {
		return ""
	}
	value := action.FormValue[strings.TrimSpace(key)]
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func (s Service) recordContext(sessionKey, threadID string, msg *feishu.InboundMessage) {
	anchor := goalapp.Anchor{SessionKey: sessionKey, ThreadID: threadID}
	if msg != nil {
		anchor.ChatID = strings.TrimSpace(msg.ChatID)
		anchor.ChatType = strings.TrimSpace(msg.ChatType)
		anchor.UserID = strings.TrimSpace(msg.UserID)
	}
	s.app.Tracker().RecordContext(anchor)
}

func (s Service) recordContextFromAction(action *feishu.CardAction, sessionKey, threadID string) {
	if action == nil {
		return
	}
	s.app.Tracker().RecordContext(goalapp.Anchor{
		SessionKey: sessionKey,
		ThreadID:   threadID,
		ChatID:     strings.TrimSpace(action.ChatID),
		UserID:     strings.TrimSpace(action.UserID),
	})
}

func OnThreadGoalUpdated(a Dependencies, goal conversation.ThreadGoal) {
	a.Tracker().NoteGoal(goal)
}

func OnThreadGoalCleared(a Dependencies, threadID string) {
	a.Tracker().ClearGoal(threadID)
}

func RenderContinuationCard(goal conversation.ThreadGoal, ordinal int) map[string]any {
	card := appcards.NewMarkdownBodyCard(renderGoalContinuationTitle(goal, ordinal), "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": renderGoalContinuationBody(goal),
	})
	return card
}

func renderGoalContinuationTitle(goal conversation.ThreadGoal, ordinal int) string {
	objective := truncate(strings.TrimSpace(goal.Objective), 96)
	if ordinal > 0 {
		if objective != "" {
			return fmt.Sprintf("Turn #%d - %s", ordinal, objective)
		}
		return fmt.Sprintf("Turn #%d", ordinal)
	}
	if objective != "" {
		return "Goal - " + objective
	}
	return "Goal continuation"
}

func renderGoalContinuationBody(goal conversation.ThreadGoal) string {
	lines := []string{
		"time: `" + formatGoalElapsedSeconds(goal.TimeUsedSeconds) + "`",
		"tokens: `" + formatGoalTokenProgress(goal) + "`",
	}
	return strings.Join(lines, "\n")
}

func formatGoalTokenProgress(goal conversation.ThreadGoal) string {
	used := formatGoalTokens(goal.TokensUsed)
	if goal.TokenBudget == nil {
		return used
	}
	return used + " / " + formatGoalTokens(*goal.TokenBudget)
}

func firstNonEmpty(values ...string) string {
	return textutil.FirstNonEmpty(values...)
}
func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
