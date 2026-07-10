package planmode

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/app/modelconfig"
	appruntime "feidex/internal/app/runtime"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	CommandUsage = "/plan | /plan on | /plan off"
	BackendCodex = appruntime.BackendCodex
)

type CodexClient interface {
	Call(ctx context.Context, method string, params any, out any) error
}

type StateProvider interface {
	Session(key string) *state.Session
	SaveSession(sess *state.Session) error
	UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error)
	Pending(id string) *state.PendingRequest
	PendingRequests() []*state.PendingRequest
	SavePending(req *state.PendingRequest) error
	UpdatePending(id string, mutate func(*state.PendingRequest)) error
	NextLocalID(prefix string) (string, error)
	CreateSubmission(sub *state.Submission) (string, error)
	QueueSubmission(sessionKey, submissionID string) error
	Submission(id string) *state.Submission
}

type App interface {
	Config() *config.Config
	ConfigMu() *sync.RWMutex
	Backend() string
	FrontendID() string
	FrontendConfigIndex() int
	Store() *state.Store
	State() StateProvider
	Feishu() appcore.FeishuClient
	CodexClient() (CodexClient, error)
	MakeSessionKey(msg *feishu.InboundMessage) string
	ReplyInThreadEnabled(chatType string) bool
	SessionHasActiveWork(sess *state.Session) bool
	ActionStringValue(action *feishu.CardAction, key string) string
	RunAsync(fn func())
	ReplyInThreadForSubmission(sub *state.Submission) bool
	SendLocalTurnFollowupCard(ctx context.Context, parentMessageID string, card map[string]any, replyInThread bool, sub *state.Submission, kind string) (string, error)
	StartNextSubmission(sessionKey string) error
	StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error)
}

func CommandPlan(a App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: %s", CommandUsage)
	}
	if len(args) == 1 {
		switch strings.TrimSpace(args[0]) {
		case "on", "off":
		default:
			return fmt.Errorf("usage: %s", CommandUsage)
		}
	}
	if a == nil || msg == nil {
		return nil
	}
	sessionKey := a.MakeSessionKey(msg)
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动线程，无法配置 plan mode")
	}
	currentMode := NormalizeThreadCollaborationMode(sess.ActiveThreadCollaborationMode)
	switch {
	case len(args) == 0 && currentMode != nil && strings.EqualFold(currentMode.Mode, "plan"):
		defaultMode, err := ResolveDefaultCodexCollaborationModeForSession(a, sess)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = defaultMode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		InvalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已关闭 plan mode，旧的计划确认已失效。")
		return a.Feishu().ReplyText(context.Background(), msg.MessageID, "当前 thread 已关闭 `plan` collaboration mode。", a.ReplyInThreadEnabled(msg.ChatType))
	case len(args) == 0:
		mode, err := ResolvePlanModeForActiveThread(a)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = mode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		InvalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已重新配置 plan mode，旧的计划确认已失效。")
		return a.Feishu().ReplyText(context.Background(), msg.MessageID, RenderPlanModeStatusText(mode), a.ReplyInThreadEnabled(msg.ChatType))
	case strings.TrimSpace(args[0]) == "off":
		defaultMode, err := ResolveDefaultCodexCollaborationModeForSession(a, sess)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = defaultMode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		InvalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已关闭 plan mode，旧的计划确认已失效。")
		return a.Feishu().ReplyText(context.Background(), msg.MessageID, "当前 thread 已关闭 `plan` collaboration mode。", a.ReplyInThreadEnabled(msg.ChatType))
	default:
		mode, err := ResolvePlanModeForActiveThread(a)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = mode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		InvalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已重新配置 plan mode，旧的计划确认已失效。")
		return a.Feishu().ReplyText(context.Background(), msg.MessageID, RenderPlanModeStatusText(mode), a.ReplyInThreadEnabled(msg.ChatType))
	}
}

func RenderPlanModeStatusText(mode *state.SessionCollaborationMode) string {
	mode = NormalizeThreadCollaborationMode(mode)
	if mode == nil {
		return "当前 thread 未开启 `plan` collaboration mode。"
	}
	lines := []string{
		"当前 thread 已开启 `plan` collaboration mode。",
		"mode: `" + mode.Mode + "`",
		"model: `" + mode.Model + "`",
	}
	if effort := strings.TrimSpace(mode.ReasoningEffort); effort != "" {
		lines = append(lines, "reasoning_effort: `"+effort+"`")
	}
	lines = append(lines, "developer_instructions: `null`")
	return strings.Join(lines, "\n")
}

func ResolvePlanModeForActiveThread(a App) (*state.SessionCollaborationMode, error) {
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	if a.Config() == nil || !a.Config().Codex.ExperimentalAPI {
		return nil, fmt.Errorf("当前 Codex runtime 未启用 experimental API，`/plan` 不可用")
	}
	client, err := a.CodexClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var listResp codexrpc.CollaborationModeListResponse
	if err := client.Call(ctx, "collaborationMode/list", map[string]any{}, &listResp); err != nil {
		return nil, fmt.Errorf("读取 collaboration mode 列表失败: %w", err)
	}
	preset, err := modelconfig.FindPlanCollaborationModePreset(listResp)
	if err != nil {
		return nil, err
	}
	model, effort, err := resolvePlanModeSettings(ctx, a, client, preset)
	if err != nil {
		return nil, err
	}
	mode := &state.SessionCollaborationMode{
		Mode:  "plan",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return NormalizeThreadCollaborationMode(mode), nil
}

func PlanModeForSession(a App, sessionKey string) *state.SessionCollaborationMode {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil {
		return nil
	}
	return NormalizeThreadCollaborationMode(sess.ActiveThreadCollaborationMode)
}

func sessionActiveThreadIDForLog(sess *state.Session) string {
	if sess == nil {
		return ""
	}
	return strings.TrimSpace(sess.ActiveThreadID)
}

func sessionActiveCollaborationModeForLog(sess *state.Session) *state.SessionCollaborationMode {
	if sess == nil {
		return nil
	}
	return sess.ActiveThreadCollaborationMode
}

func sessionBackendCollaborationModeForLog(sess *state.Session, backend string) *state.SessionCollaborationMode {
	if sess == nil || len(sess.BackendThreads) == 0 {
		return nil
	}
	snapshot, ok := sess.BackendThreads[strings.TrimSpace(backend)]
	if !ok {
		return nil
	}
	return snapshot.CollaborationMode
}

func ResolveDefaultCodexCollaborationModeForSession(a App, sess *state.Session) (*state.SessionCollaborationMode, error) {
	if mode := defaultCodexCollaborationModeForSession(a, sess); mode != nil {
		return mode, nil
	}
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	client, err := a.CodexClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	model, effort, err := resolveDefaultCollaborationModeSettings(ctx, a, client)
	if err != nil {
		return nil, fmt.Errorf("无法解析 default collaboration mode model: %w", err)
	}
	mode := &state.SessionCollaborationMode{
		Mode:  "default",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return NormalizeThreadCollaborationMode(mode), nil
}

func defaultCodexCollaborationModeForSession(a App, sess *state.Session) *state.SessionCollaborationMode {
	model := ""
	effort := ""
	if a != nil && a.Config() != nil {
		model = strings.TrimSpace(modelconfig.ConfiguredGlobalModel(a.Config()))
		effort = strings.TrimSpace(modelconfig.ConfiguredGlobalReasoningEffort(a.Config()))
	}
	if mode := NormalizeThreadCollaborationMode(sessionActiveCollaborationModeForLog(sess)); mode != nil && strings.EqualFold(mode.Mode, "default") {
		if model == "" {
			model = strings.TrimSpace(mode.Model)
		}
		if effort == "" {
			effort = strings.TrimSpace(mode.ReasoningEffort)
		}
	}
	if mode := NormalizeThreadCollaborationMode(sessionBackendCollaborationModeForLog(sess, BackendCodex)); mode != nil && strings.EqualFold(mode.Mode, "default") {
		if model == "" {
			model = strings.TrimSpace(mode.Model)
		}
		if effort == "" {
			effort = strings.TrimSpace(mode.ReasoningEffort)
		}
	}
	if mode := NormalizeThreadCollaborationMode(sessionActiveCollaborationModeForLog(sess)); mode != nil && canReuseCollaborationModeModelForDefault(a, mode) {
		if model == "" {
			model = strings.TrimSpace(mode.Model)
		}
	}
	if model == "" {
		if mode := NormalizeThreadCollaborationMode(sessionBackendCollaborationModeForLog(sess, BackendCodex)); mode != nil && canReuseCollaborationModeModelForDefault(a, mode) {
			model = strings.TrimSpace(mode.Model)
		}
	}
	if model == "" {
		slog.Debug("plan mode disable could not build default collaboration mode",
			"backend", appcore.ConfiguredBackend(a),
			"active_thread_id", sessionActiveThreadIDForLog(sess),
		)
		return nil
	}
	mode := &state.SessionCollaborationMode{
		Mode:  "default",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return NormalizeThreadCollaborationMode(mode)
}

func canReuseCollaborationModeModelForDefault(a App, mode *state.SessionCollaborationMode) bool {
	mode = NormalizeThreadCollaborationMode(mode)
	if mode == nil {
		return false
	}
	if !strings.EqualFold(mode.Mode, "plan") {
		return true
	}
	if a == nil || a.Config() == nil {
		return true
	}
	return strings.TrimSpace(modelconfig.ConfiguredPlanModel(a.Config())) == ""
}

func PlanModeTitleForSession(a App, sessionKey, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	title = sessionWorkspaceTitleForSession(a, sessionKey, title)
	mode := PlanModeForSession(a, sessionKey)
	if mode == nil || !strings.EqualFold(mode.Mode, "plan") {
		return title
	}
	return prependTitlePrefix(title, "[plan]")
}

func ContentCardTitleForSubmission(a App, sub *state.Submission, title string) string {
	if sub == nil {
		return strings.TrimSpace(title)
	}
	return ContentCardTitleForSession(a, sub.SessionKey, sub.WorkspaceID, title)
}

func ContentCardTitleForSession(a App, sessionKey, workspaceID, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if strings.TrimSpace(workspaceID) == "" && a != nil && sessionKey != "" {
		if sess := a.State().Session(sessionKey); sess != nil {
			workspaceID = strings.TrimSpace(sess.WorkspaceID)
		}
	}
	planMode := false
	if mode := PlanModeForSession(a, sessionKey); mode != nil {
		planMode = strings.EqualFold(mode.Mode, "plan")
	}
	return normalizeContentCardTitle(title, workspaceID, planMode)
}

func normalizeContentCardTitle(title, workspaceID string, planMode bool) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	prefixes, rest := splitLeadingTitlePrefixes(title)
	desired := make([]string, 0, 2+len(prefixes))
	if ws := strings.TrimSpace(workspaceID); ws != "" {
		desired = append(desired, "["+ws+"]")
	}
	if planMode {
		desired = append(desired, "[plan]")
	}
	for _, prefix := range prefixes {
		if titlePrefixAlreadyPresent(desired, prefix) {
			continue
		}
		desired = append(desired, prefix)
	}
	if len(desired) == 0 {
		return rest
	}
	if rest == "" {
		return strings.Join(desired, " ")
	}
	return strings.Join(desired, " ") + " " + rest
}

func titlePrefixAlreadyPresent(prefixes []string, candidate string) bool {
	for _, existing := range prefixes {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func sessionWorkspaceTitleForSession(a App, sessionKey, title string) string {
	title = strings.TrimSpace(title)
	if title == "" || a == nil || strings.TrimSpace(sessionKey) == "" {
		return title
	}
	sess := a.State().Session(sessionKey)
	if sess == nil {
		return title
	}
	ws := strings.TrimSpace(sess.WorkspaceID)
	if ws == "" {
		return title
	}
	return prependTitlePrefix(title, "["+ws+"]")
}

func prependTitlePrefix(title, prefix string) string {
	title = strings.TrimSpace(title)
	prefix = strings.TrimSpace(prefix)
	if title == "" {
		return ""
	}
	if prefix == "" {
		return title
	}
	leadingPrefixes, rest := splitLeadingTitlePrefixes(title)
	for _, existing := range leadingPrefixes {
		if strings.EqualFold(existing, prefix) {
			return title
		}
	}
	parts := append(append([]string(nil), leadingPrefixes...), prefix)
	if rest == "" {
		return strings.Join(parts, " ")
	}
	return strings.Join(parts, " ") + " " + rest
}

func splitLeadingTitlePrefixes(title string) (prefixes []string, rest string) {
	rest = strings.TrimSpace(title)
	for {
		if !strings.HasPrefix(rest, "[") {
			break
		}
		end := strings.Index(rest, "] ")
		if end < 0 {
			if strings.HasSuffix(rest, "]") {
				prefixes = append(prefixes, rest)
				rest = ""
			}
			break
		}
		prefixes = append(prefixes, rest[:end+1])
		rest = strings.TrimSpace(rest[end+2:])
		if rest == "" {
			break
		}
	}
	return prefixes, rest
}

func resolvePlanModeSettings(ctx context.Context, a App, client CodexClient, preset *codexrpc.CollaborationModeMask) (model string, effort string, err error) {
	model = strings.TrimSpace(modelconfig.ConfiguredPlanModel(a.Config()))
	if model == "" {
		model = strings.TrimSpace(modelconfig.ConfiguredGlobalModel(a.Config()))
	}
	effort = strings.TrimSpace(modelconfig.ConfiguredPlanReasoningEffort(a.Config()))
	if effort == "" && preset != nil && preset.ReasoningEffort != nil {
		effort = strings.TrimSpace(*preset.ReasoningEffort)
	}
	if model != "" {
		return model, effort, nil
	}
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{
		"limit":         20,
		"includeHidden": false,
	}, &result); err != nil {
		return "", "", fmt.Errorf("读取 model 列表失败: %w", err)
	}
	entry, resolvedEffort := modelconfig.EffectivePlanConfiguredModelAndEffort(a.Config(), result, preset)
	if entry == nil {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法开启 `/plan`")
	}
	model = appcore.FirstNonEmpty(strings.TrimSpace(entry.ID), strings.TrimSpace(entry.Model))
	if model == "" {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法开启 `/plan`")
	}
	return model, resolvedEffort, nil
}

func resolveDefaultCollaborationModeSettings(ctx context.Context, a App, client CodexClient) (model string, effort string, err error) {
	model = strings.TrimSpace(modelconfig.ConfiguredGlobalModel(a.Config()))
	effort = strings.TrimSpace(modelconfig.ConfiguredGlobalReasoningEffort(a.Config()))
	if model != "" {
		return model, effort, nil
	}
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{
		"limit":         20,
		"includeHidden": false,
	}, &result); err != nil {
		return "", "", fmt.Errorf("读取 model 列表失败: %w", err)
	}
	entry, resolvedEffort := modelconfig.EffectiveConfiguredModelAndEffort(a.Config(), result)
	if entry == nil {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法恢复 default collaboration mode")
	}
	model = appcore.FirstNonEmpty(strings.TrimSpace(entry.ID), strings.TrimSpace(entry.Model))
	if model == "" {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法恢复 default collaboration mode")
	}
	if effort != "" {
		effort = strings.TrimSpace(resolvedEffort)
	}
	return model, effort, nil
}

func CodexCollaborationModeForTurnStart(a App, sessionKey, threadID string) *codexrpc.CollaborationMode {
	if a == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil
	}
	mode := DefaultCollaborationModeWithConfiguredEffort(a, sess.ActiveThreadCollaborationMode)
	return CodexCollaborationModeFromState(mode)
}

func CodexCollaborationModeFromState(mode *state.SessionCollaborationMode) *codexrpc.CollaborationMode {
	mode = NormalizeThreadCollaborationMode(mode)
	if mode == nil {
		return nil
	}
	var reasoningEffort *string
	if value := strings.TrimSpace(mode.ReasoningEffort); value != "" {
		reasoningEffort = &value
	}
	return &codexrpc.CollaborationMode{
		Mode: mode.Mode,
		Settings: codexrpc.CollaborationModeSettings{
			DeveloperInstructions: nil,
			Model:                 mode.Model,
			ReasoningEffort:       reasoningEffort,
		},
	}
}

func DefaultCollaborationModeWithConfiguredEffort(a App, mode *state.SessionCollaborationMode) *state.SessionCollaborationMode {
	mode = NormalizeThreadCollaborationMode(mode)
	if mode == nil || !strings.EqualFold(mode.Mode, "default") || strings.TrimSpace(mode.ReasoningEffort) != "" {
		return mode
	}
	if a == nil || a.Config() == nil {
		return mode
	}
	effort := strings.TrimSpace(modelconfig.ConfiguredGlobalReasoningEffort(a.Config()))
	if effort == "" {
		return mode
	}
	cp := *mode
	cp.ReasoningEffort = effort
	return NormalizeThreadCollaborationMode(&cp)
}

func NormalizeThreadCollaborationMode(mode *state.SessionCollaborationMode) *state.SessionCollaborationMode {
	if mode == nil {
		return nil
	}
	cp := *mode
	cp.Mode = strings.TrimSpace(cp.Mode)
	cp.Model = strings.TrimSpace(cp.Model)
	cp.ReasoningEffort = strings.TrimSpace(cp.ReasoningEffort)
	if cp.Mode == "" || cp.Model == "" {
		return nil
	}
	return &cp
}

func derefStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
