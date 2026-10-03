package goalcmd

import (
	"context"
	"feidex/internal/app/appcore"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	appcards "feidex/internal/adapter/feishu/cards"
	"feidex/internal/application/backendops"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	CommandUsage          = "/goal | /goal <objective> | /goal pause | /goal resume | /goal clear | /goal edit"
	MaxObjectiveRunes     = 4000
	SubmissionKind        = "goal"
	ContinuationInputText = "[goal continuation]"
)

type CodexClient interface {
	GetGoal(context.Context, string) (backendops.GoalLookup, error)
	SetGoal(context.Context, backendops.GoalUpdate) (backendops.GoalResult, error)
	ClearGoal(context.Context, string) (backendops.GoalCleared, error)
}

type StateProvider interface {
	Session(key string) *conversation.Session
	Sessions() []*conversation.Session
	SaveSession(sess *conversation.Session) error
	CreateSubmission(sub *domainsubmission.Submission) (string, error)
	UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error)
	DeleteSubmission(id string)
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
	StateProvider                 StateProvider
	Outbound                      Outbound
	CardRenderer                  CardRenderer
	CodexClientProvider           func() (CodexClient, error)
	GoalTracker                   *Tracker
	MakeSessionKeyFn              func(*feishu.InboundMessage) string
	ReplyInThreadEnabledFn        func(string) bool
	MenuCardBodyForSessionFn      func(string, string, string) string
	ActionStringValueFn           func(*feishu.CardAction, string) string
	ActionSessionKeyFn            func(*feishu.CardAction) string
	CompleteMenuCommandFn         func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
	DefaultWorkspaceIDFn          func() string
	SessionBelongsToFrontendFn    func(string) bool
	BindTurnSubmissionFn          func(string, string, string, string)
	MarkTurnStartedAtFn           func(string, time.Time)
	RecordSubmissionSourceLinksFn func(*domainsubmission.Submission)
	RecordRootTurnBindingFn       func(string, string, string, string)
	NoteTurnStartedFn             func(string, *domainsubmission.Submission)
	MarkSessionThreadLiveFn       func(string, string)
	ContextProvider               interface{ Context() context.Context }
}

func (d Dependencies) State() StateProvider   { return d.StateProvider }
func (d Dependencies) Messaging() Outbound    { return d.Outbound }
func (d Dependencies) Renderer() CardRenderer { return d.CardRenderer }
func (d Dependencies) CodexClient() (CodexClient, error) {
	if d.CodexClientProvider == nil {
		return nil, fmt.Errorf("codex client unavailable")
	}
	return d.CodexClientProvider()
}
func (d Dependencies) Tracker() *Tracker { return d.GoalTracker }
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
func (d Dependencies) DefaultWorkspaceID() string {
	if d.DefaultWorkspaceIDFn == nil {
		return "default"
	}
	return d.DefaultWorkspaceIDFn()
}
func (d Dependencies) SessionBelongsToFrontend(s string) bool {
	return d.SessionBelongsToFrontendFn == nil || d.SessionBelongsToFrontendFn(s)
}
func (d Dependencies) BindTurnSubmission(a, b, c, e string) {
	if d.BindTurnSubmissionFn != nil {
		d.BindTurnSubmissionFn(a, b, c, e)
	}
}
func (d Dependencies) MarkTurnStartedAt(a string, t time.Time) {
	if d.MarkTurnStartedAtFn != nil {
		d.MarkTurnStartedAtFn(a, t)
	}
}
func (d Dependencies) RecordSubmissionSourceLinks(s *domainsubmission.Submission) {
	if d.RecordSubmissionSourceLinksFn != nil {
		d.RecordSubmissionSourceLinksFn(s)
	}
}
func (d Dependencies) RecordRootTurnBinding(a, b, c, e string) {
	if d.RecordRootTurnBindingFn != nil {
		d.RecordRootTurnBindingFn(a, b, c, e)
	}
}
func (d Dependencies) NoteTurnStarted(a string, s *domainsubmission.Submission) {
	if d.NoteTurnStartedFn != nil {
		d.NoteTurnStartedFn(a, s)
	}
}
func (d Dependencies) MarkSessionThreadLive(a, b string) {
	if d.MarkSessionThreadLiveFn != nil {
		d.MarkSessionThreadLiveFn(a, b)
	}
}
func (d Dependencies) Context() context.Context {
	if d.ContextProvider != nil {
		if c := d.ContextProvider.Context(); c != nil {
			return c
		}
	}
	return context.Background()
}

type Tracker struct {
	mu                 sync.Mutex
	goals              map[string]conversation.ThreadGoal
	anchors            map[string]Anchor
	continuationCounts map[string]int
}

type Anchor struct {
	SessionKey string
	ThreadID   string
	MessageID  string
	ChatID     string
	ChatType   string
	UserID     string
}

func NewTracker() *Tracker {
	return &Tracker{
		goals:              map[string]conversation.ThreadGoal{},
		anchors:            map[string]Anchor{},
		continuationCounts: map[string]int{},
	}
}

func (t *Tracker) NoteGoal(goal conversation.ThreadGoal) {
	if t == nil {
		return
	}
	threadID := strings.TrimSpace(goal.ThreadID)
	if threadID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.goals == nil {
		t.goals = map[string]conversation.ThreadGoal{}
	}
	t.goals[threadID] = goal
}

func (t *Tracker) ClearGoal(threadID string) {
	if t == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.goals, threadID)
	delete(t.continuationCounts, threadID)
}

func (t *Tracker) ActiveGoal(threadID string) (conversation.ThreadGoal, bool) {
	if t == nil {
		return conversation.ThreadGoal{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return conversation.ThreadGoal{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	goal, ok := t.goals[threadID]
	return goal, ok && goal.Status == conversation.ThreadGoalStatusActive
}

func (t *Tracker) RecordAnchor(anchor Anchor) {
	if t == nil {
		return
	}
	anchor.ThreadID = strings.TrimSpace(anchor.ThreadID)
	anchor.SessionKey = strings.TrimSpace(anchor.SessionKey)
	anchor.MessageID = strings.TrimSpace(anchor.MessageID)
	if anchor.ThreadID == "" || anchor.SessionKey == "" || anchor.MessageID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.anchors == nil {
		t.anchors = map[string]Anchor{}
	}
	t.anchors[anchor.ThreadID] = anchor
}

func (t *Tracker) RecordContext(anchor Anchor) {
	if t == nil {
		return
	}
	anchor.ThreadID = strings.TrimSpace(anchor.ThreadID)
	anchor.SessionKey = strings.TrimSpace(anchor.SessionKey)
	if anchor.ThreadID == "" || anchor.SessionKey == "" {
		return
	}
	anchor.MessageID = ""
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.anchors == nil {
		t.anchors = map[string]Anchor{}
	}
	if existing, ok := t.anchors[anchor.ThreadID]; ok {
		anchor.ChatID = appcore.FirstNonEmpty(strings.TrimSpace(anchor.ChatID), strings.TrimSpace(existing.ChatID))
		anchor.ChatType = appcore.FirstNonEmpty(strings.TrimSpace(anchor.ChatType), strings.TrimSpace(existing.ChatType))
		anchor.UserID = appcore.FirstNonEmpty(strings.TrimSpace(anchor.UserID), strings.TrimSpace(existing.UserID))
	}
	t.anchors[anchor.ThreadID] = anchor
}

func (t *Tracker) Anchor(threadID string) (Anchor, bool) {
	if t == nil {
		return Anchor{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return Anchor{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	anchor, ok := t.anchors[threadID]
	return anchor, ok
}

func (t *Tracker) NextContinuationOrdinal(threadID string) int {
	if t == nil {
		return 0
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.continuationCounts == nil {
		t.continuationCounts = map[string]int{}
	}
	t.continuationCounts[threadID]++
	return t.continuationCounts[threadID]
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
		_, err = a.Outbound.ReplyCard(appcore.Context(s.app), msg.MessageID, card, a.ReplyInThreadEnabled(msg.ChatType))
		s.recordContext(sessionKey, threadID, msg)
		return err
	default:
		objective := strings.TrimSpace(tail)
		if err := validateGoalObjective(objective); err != nil {
			return err
		}
		existing, err := s.threadGoalGet(threadID)
		if err != nil {
			return fmt.Errorf("%s", goalFriendlyError("读取", err))
		}
		if existing != nil && shouldConfirmBeforeReplacingGoal(*existing) {
			card := s.renderGoalReplaceConfirmCard(sessionKey, threadID, *existing, objective)
			_, err := a.Outbound.ReplyCard(appcore.Context(s.app), msg.MessageID, card, a.ReplyInThreadEnabled(msg.ChatType))
			s.recordContext(sessionKey, threadID, msg)
			return err
		}
		if existing != nil && existing.Status == conversation.ThreadGoalStatusComplete {
			if _, err := s.threadGoalClear(threadID); err != nil {
				return fmt.Errorf("%s", goalFriendlyError("替换", err))
			}
		}
		if _, err := s.threadGoalSetObjective(threadID, objective, conversation.ThreadGoalStatusActive, nil); err != nil {
			return fmt.Errorf("%s", goalFriendlyError("设置", err))
		}
		return s.replyGoalSetText(msg, sessionKey, threadID)
	}
}

func (s Service) replyGoalSetText(msg *feishu.InboundMessage, sessionKey, threadID string) error {
	if s.app.StateProvider == nil || s.app.Outbound == nil || msg == nil {
		return nil
	}
	s.recordContext(sessionKey, threadID, msg)
	return s.app.Outbound.ReplyText(appcore.Context(s.app), msg.MessageID, "已设置 goal。", s.app.ReplyInThreadEnabled(msg.ChatType))
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

func validateGoalObjective(objective string) error {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return fmt.Errorf("goal objective must not be empty\n\nusage: %s", CommandUsage)
	}
	if count := len([]rune(objective)); count > MaxObjectiveRunes {
		return fmt.Errorf("goal objective is too long: %d characters. Limit: %d characters. Put longer instructions in a file and refer to that file in the goal", count, MaxObjectiveRunes)
	}
	return nil
}

func shouldConfirmBeforeReplacingGoal(goal conversation.ThreadGoal) bool {
	switch goal.Status {
	case conversation.ThreadGoalStatusComplete:
		return false
	case conversation.ThreadGoalStatusActive,
		conversation.ThreadGoalStatusPaused,
		conversation.ThreadGoalStatusBlocked,
		conversation.ThreadGoalStatusUsageLimited,
		conversation.ThreadGoalStatusBudgetLimited:
		return true
	default:
		return true
	}
}

func editedGoalStatus(status conversation.ThreadGoalStatus) conversation.ThreadGoalStatus {
	switch status {
	case conversation.ThreadGoalStatusActive,
		conversation.ThreadGoalStatusPaused,
		conversation.ThreadGoalStatusBlocked,
		conversation.ThreadGoalStatusUsageLimited:
		return status
	default:
		return conversation.ThreadGoalStatusActive
	}
}

func (s Service) threadGoalGet(threadID string) (*conversation.ThreadGoal, error) {
	client, err := s.app.CodexClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(appcore.Context(s.app), 20*time.Second)
	defer cancel()
	resp, err := client.GetGoal(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if resp.Goal == nil {
		s.app.Tracker().ClearGoal(threadID)
		return nil, nil
	}
	s.app.Tracker().NoteGoal(*resp.Goal)
	return resp.Goal, nil
}

func (s Service) threadGoalSetStatus(threadID string, status conversation.ThreadGoalStatus) (*conversation.ThreadGoal, error) {
	return s.threadGoalSetObjective(threadID, "", status, nil)
}

func (s Service) threadGoalSetObjective(threadID, objective string, status conversation.ThreadGoalStatus, tokenBudget *int64) (*conversation.ThreadGoal, error) {
	client, err := s.app.CodexClient()
	if err != nil {
		return nil, err
	}
	params := backendops.GoalUpdate{ThreadID: strings.TrimSpace(threadID)}
	if strings.TrimSpace(objective) != "" {
		trimmed := strings.TrimSpace(objective)
		params.Objective = &trimmed
	}
	if status != "" {
		statusCopy := status
		params.Status = &statusCopy
	}
	if tokenBudget != nil {
		params.TokenBudget = newBudgetUpdate(tokenBudget)
	}
	ctx, cancel := context.WithTimeout(appcore.Context(s.app), 20*time.Second)
	defer cancel()
	resp, err := client.SetGoal(ctx, params)
	if err != nil {
		return nil, err
	}
	s.app.Tracker().NoteGoal(resp.Goal)
	return &resp.Goal, nil
}

func (s Service) threadGoalClear(threadID string) (bool, error) {
	client, err := s.app.CodexClient()
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(appcore.Context(s.app), 20*time.Second)
	defer cancel()
	resp, err := client.ClearGoal(ctx, threadID)
	if err != nil {
		return false, err
	}
	s.app.Tracker().ClearGoal(threadID)
	return resp.Cleared, nil
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
	_, err := s.app.Outbound.ReplyCard(appcore.Context(s.app), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
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
	_, err := s.app.Outbound.ReplyCard(appcore.Context(s.app), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
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
			Text:  appcore.FirstNonEmpty(opts.SubmitText, "保存"),
			Type:  "primary",
			Name:  "goal_edit_submit",
			Value: opts.SubmitValue,
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "goal_edit_cancel",
			Value: map[string]any{"action": appcore.FirstNonEmpty(opts.CancelAction, "menu.goal"), "session_key": opts.SessionKey},
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
	if _, err := s.threadGoalClear(threadID); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: goalFriendlyError("替换", err)}}, nil
	}
	goal, err := s.threadGoalSetObjective(threadID, objective, conversation.ThreadGoalStatusActive, nil)
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
	anchor := Anchor{SessionKey: sessionKey, ThreadID: threadID}
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
	s.app.Tracker().RecordContext(Anchor{
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

func (s Service) BindGoalContinuationTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return false
	}
	goal, ok := s.app.Tracker().ActiveGoal(threadID)
	if !ok {
		return false
	}
	sessionKey, sess := s.findGoalContinuationSession(threadID)
	if sess == nil || sessionKey == "" {
		return false
	}
	if conversation.HasActiveOperations(sess) {
		return false
	}
	anchor, ok := s.sendGoalContinuationAnchor(sessionKey, threadID, turnID, sess, goal)
	if !ok {
		return false
	}
	triggerMessageID := anchor.MessageID
	workspaceID := appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), s.app.DefaultWorkspaceID())
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          workspaceID,
		ThreadID:             threadID,
		TurnID:               turnID,
		UserID:               strings.TrimSpace(sess.OwnerUserID),
		ChatID:               strings.TrimSpace(anchor.ChatID),
		TriggerMessageID:     triggerMessageID,
		SourceMessageIDs:     goalUniqueNonEmpty([]string{triggerMessageID}),
		SourceRootMessageIDs: goalUniqueNonEmpty([]string{triggerMessageID}),
		InputText:            ContinuationInputText,
		Kind:                 SubmissionKind,
		Status:               domainsubmission.SubmissionStatusRunning.String(),
	}
	id, err := s.app.State().CreateSubmission(sub)
	if err != nil || strings.TrimSpace(id) == "" {
		return false
	}
	sub.ID = id
	updatedSess, err := s.app.State().UpdateSession(sessionKey, func(current *conversation.Session) {
		if current == nil {
			return
		}
		conversation.UpsertActiveOperation(current, conversation.SessionActiveOperation{
			Kind:         conversation.OpKindSubmission,
			SubmissionID: id,
			ThreadID:     threadID,
			TurnID:       turnID,
		})
		current.Status = conversation.SessionStatusTurnInProgress.String()
		conversation.SetThreadContext(current, workspaceID, threadID, current.ActiveThreadName, current.ActiveThreadPreview)
	})
	if err != nil || updatedSess == nil {
		s.app.State().DeleteSubmission(id)
		return false
	}
	s.app.BindTurnSubmission(threadID, turnID, sessionKey, id)
	s.app.MarkTurnStartedAt(turnID, time.Now())
	s.app.RecordSubmissionSourceLinks(sub)
	s.app.RecordRootTurnBinding(triggerMessageID, sessionKey, threadID, turnID)
	s.app.NoteTurnStarted(sessionKey, sub)
	s.app.MarkSessionThreadLive(sessionKey, threadID)
	return true
}

func (s Service) sendGoalContinuationAnchor(sessionKey, threadID, turnID string, sess *conversation.Session, goal conversation.ThreadGoal) (Anchor, bool) {
	if s.app.StateProvider == nil || s.app.Outbound == nil || sess == nil {
		return Anchor{}, false
	}
	anchor := Anchor{
		SessionKey: sessionKey,
		ThreadID:   threadID,
		ChatID:     strings.TrimSpace(sess.ChatID),
		ChatType:   strings.TrimSpace(sess.ChatType),
		UserID:     strings.TrimSpace(sess.OwnerUserID),
	}
	if recorded, ok := s.app.Tracker().Anchor(threadID); ok {
		anchor.ChatID = appcore.FirstNonEmpty(anchor.ChatID, strings.TrimSpace(recorded.ChatID))
		anchor.ChatType = appcore.FirstNonEmpty(anchor.ChatType, strings.TrimSpace(recorded.ChatType))
		anchor.UserID = appcore.FirstNonEmpty(anchor.UserID, strings.TrimSpace(recorded.UserID))
	}
	if anchor.ChatID == "" {
		return Anchor{}, false
	}
	ordinal := s.app.Tracker().NextContinuationOrdinal(threadID)
	card := s.renderGoalContinuationCard(sessionKey, threadID, turnID, goal, ordinal)
	messageID, err := s.app.Outbound.SendCard(appcore.Context(s.app), anchor.ChatID, card)
	if err != nil {
		return Anchor{}, false
	}
	anchor.MessageID = strings.TrimSpace(messageID)
	if anchor.MessageID == "" {
		return Anchor{}, false
	}
	s.app.Tracker().RecordAnchor(anchor)
	return anchor, true
}

func (s Service) renderGoalContinuationCard(_, _, _ string, goal conversation.ThreadGoal, ordinal int) map[string]any {
	card := appcards.NewMarkdownBodyCard(renderGoalContinuationTitle(goal, ordinal), "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": renderGoalContinuationBody(goal),
	})
	return card
}

func renderGoalContinuationTitle(goal conversation.ThreadGoal, ordinal int) string {
	objective := appcore.Truncate(strings.TrimSpace(goal.Objective), 96)
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

func (s Service) findGoalContinuationSession(threadID string) (string, *conversation.Session) {
	if anchor, ok := s.app.Tracker().Anchor(threadID); ok {
		if sess := s.app.State().Session(anchor.SessionKey); sess != nil && strings.TrimSpace(sess.ActiveThreadID) == threadID {
			return anchor.SessionKey, sess
		}
	}
	for _, sess := range s.app.State().Sessions() {
		if sess == nil || !s.app.SessionBelongsToFrontend(sess.Key) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == threadID {
			return sess.Key, sess
		}
	}
	return "", nil
}

func goalUniqueNonEmpty(items []string) []string {
	trimmed := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			trimmed = append(trimmed, item)
		}
	}
	return appcore.UniqueStrings(trimmed)
}

func newBudgetUpdate(value *int64) *backendops.BudgetUpdate {
	if value == nil {
		return &backendops.BudgetUpdate{}
	}
	cp := *value
	return &backendops.BudgetUpdate{Value: &cp}
}
