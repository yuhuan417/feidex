package planmode

import (
	"context"
	feishutransport "feidex/internal/adapter/feishu/transport"
	"feidex/internal/app/appcore"
	"feidex/internal/app/modelconfig"
	"feidex/internal/application/workspace"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	catalog "feidex/internal/domain/modelconfig"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	CommandUsage = "/plan | /plan on | /plan off"
	BackendCodex = domainbackend.BackendCodex
)

type CodexClient interface {
	ListModels(context.Context, int) (catalog.ModelListResult, error)
	ListCollaborationModes(context.Context) (catalog.CollaborationModeListResponse, error)
}

// Dependencies is the plan-mode capability set. It keeps plan-mode logic
// independent from the application orchestrator while making every runtime
// capability explicit at the composition boundary.
type Dependencies struct {
	ConfigProvider interface {
		appcore.ConfigurationSource
		appcore.FrontendIdentity
		Store() *state.Store
		WorkspaceSelection() workspace.SelectionService
	}
	ContextProvider              interface{ Context() context.Context }
	StateProvider                StateProvider
	FeishuClient                 feishutransport.Client
	CodexClientProvider          func() (CodexClient, error)
	MakeSessionKeyFn             func(*feishu.InboundMessage) string
	ReplyInThreadEnabledFn       func(string) bool
	SessionHasActiveWorkFn       func(*conversation.Session) bool
	EffectivePlanSettingsFn      func(*conversation.Session) (string, string)
	ActionStringValueFn          func(*feishu.CardAction, string) string
	RunAsyncFn                   func(func())
	ReplyInThreadForSubmissionFn func(*domainsubmission.Submission) bool
	SendLocalTurnFollowupCardFn  func(context.Context, string, map[string]any, bool, *domainsubmission.Submission, string) (string, error)
	StartNextSubmissionFn        func(string) error
	StartWorkspaceThreadFn       func(string, *conversation.Session, *config.Workspace) (*appworkspace.ThreadBinding, error)
}

func (d Dependencies) Config() *config.Config {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.Config()
}
func (d Dependencies) ConfigMu() *sync.RWMutex {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.ConfigMu()
}
func (d Dependencies) Backend() string {
	if d.ConfigProvider == nil {
		return ""
	}
	return d.ConfigProvider.Backend()
}
func (d Dependencies) FrontendID() string {
	if d.ConfigProvider == nil {
		return ""
	}
	return d.ConfigProvider.FrontendID()
}
func (d Dependencies) FrontendConfigIndex() int {
	if d.ConfigProvider == nil {
		return -1
	}
	return d.ConfigProvider.FrontendConfigIndex()
}
func (d Dependencies) Store() *state.Store {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.Store()
}
func (d Dependencies) Context() context.Context {
	if d.ContextProvider != nil {
		if ctx := d.ContextProvider.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}
func (d Dependencies) State() StateProvider           { return d.StateProvider }
func (d Dependencies) Feishu() feishutransport.Client { return d.FeishuClient }
func (d Dependencies) CodexClient() (CodexClient, error) {
	if d.CodexClientProvider == nil {
		return nil, fmt.Errorf("codex client unavailable")
	}
	return d.CodexClientProvider()
}
func (d Dependencies) MakeSessionKey(m *feishu.InboundMessage) string {
	if d.MakeSessionKeyFn == nil {
		return ""
	}
	return d.MakeSessionKeyFn(m)
}
func (d Dependencies) ReplyInThreadEnabled(v string) bool {
	return d.ReplyInThreadEnabledFn != nil && d.ReplyInThreadEnabledFn(v)
}
func (d Dependencies) SessionHasActiveWork(s *conversation.Session) bool {
	return d.SessionHasActiveWorkFn != nil && d.SessionHasActiveWorkFn(s)
}
func (d Dependencies) EffectivePlanSettings(s *conversation.Session) (string, string) {
	if d.EffectivePlanSettingsFn == nil {
		return "", ""
	}
	return d.EffectivePlanSettingsFn(s)
}
func (d Dependencies) ActionStringValue(a *feishu.CardAction, k string) string {
	if d.ActionStringValueFn == nil {
		return ""
	}
	return d.ActionStringValueFn(a, k)
}
func (d Dependencies) RunAsync(fn func()) {
	if d.RunAsyncFn != nil {
		d.RunAsyncFn(fn)
	} else if fn != nil {
		go fn()
	}
}
func (d Dependencies) ReplyInThreadForSubmission(s *domainsubmission.Submission) bool {
	return d.ReplyInThreadForSubmissionFn != nil && d.ReplyInThreadForSubmissionFn(s)
}
func (d Dependencies) SendLocalTurnFollowupCard(c context.Context, p string, card map[string]any, r bool, s *domainsubmission.Submission, k string) (string, error) {
	if d.SendLocalTurnFollowupCardFn == nil {
		return "", fmt.Errorf("follow-up card unavailable")
	}
	return d.SendLocalTurnFollowupCardFn(c, p, card, r, s, k)
}
func (d Dependencies) StartNextSubmission(k string) error {
	if d.StartNextSubmissionFn == nil {
		return fmt.Errorf("submission starter unavailable")
	}
	return d.StartNextSubmissionFn(k)
}
func (d Dependencies) StartWorkspaceThread(k string, s *conversation.Session, w *config.Workspace) (*appworkspace.ThreadBinding, error) {
	if d.StartWorkspaceThreadFn == nil {
		return nil, fmt.Errorf("workspace thread starter unavailable")
	}
	return d.StartWorkspaceThreadFn(k, s, w)
}

type StateProvider interface {
	Session(key string) *conversation.Session
	SaveSession(sess *conversation.Session) error
	UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error)
	Pending(id string) *state.PendingRequest
	PendingRequests() []*state.PendingRequest
	SavePending(req *state.PendingRequest) error
	UpdatePending(id string, mutate func(*state.PendingRequest)) error
	NextLocalID(prefix string) (string, error)
	CreateSubmission(sub *domainsubmission.Submission) (string, error)
	QueueSubmission(sessionKey, submissionID string) error
	Submission(id string) *domainsubmission.Submission
}

type PlanSettingsProvider interface {
	EffectivePlanSettings(sess *conversation.Session) (model, effort string)
}

func CommandPlan(a Dependencies, msg *feishu.InboundMessage, args []string) error {
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
	if a.ConfigProvider == nil || msg == nil {
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
		return a.Feishu().ReplyText(appcore.Context(a), msg.MessageID, "当前 thread 已关闭 `plan` collaboration mode。", a.ReplyInThreadEnabled(msg.ChatType))
	case len(args) == 0:
		mode, err := ResolvePlanModeForSession(a, sess)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = mode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		InvalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已重新配置 plan mode，旧的计划确认已失效。")
		return a.Feishu().ReplyText(appcore.Context(a), msg.MessageID, RenderPlanModeStatusText(mode), a.ReplyInThreadEnabled(msg.ChatType))
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
		return a.Feishu().ReplyText(appcore.Context(a), msg.MessageID, "当前 thread 已关闭 `plan` collaboration mode。", a.ReplyInThreadEnabled(msg.ChatType))
	default:
		mode, err := ResolvePlanModeForSession(a, sess)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = mode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		InvalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已重新配置 plan mode，旧的计划确认已失效。")
		return a.Feishu().ReplyText(appcore.Context(a), msg.MessageID, RenderPlanModeStatusText(mode), a.ReplyInThreadEnabled(msg.ChatType))
	}
}

func RenderPlanModeStatusText(mode *conversation.SessionCollaborationMode) string {
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

func ResolvePlanModeForActiveThread(a Dependencies) (*conversation.SessionCollaborationMode, error) {
	return ResolvePlanModeForSession(a, nil)
}

func ResolvePlanModeForSession(a Dependencies, sess *conversation.Session) (*conversation.SessionCollaborationMode, error) {
	if a.ConfigProvider == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	if a.Config() == nil || !a.Config().Codex.ExperimentalAPI {
		return nil, fmt.Errorf("当前 Codex runtime 未启用 experimental API，`/plan` 不可用")
	}
	client, err := a.CodexClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(appcore.Context(a), 20*time.Second)
	defer cancel()

	listResp, err := client.ListCollaborationModes(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取 collaboration mode 列表失败: %w", err)
	}
	preset, err := modelconfig.FindPlanCollaborationModePreset(listResp)
	if err != nil {
		return nil, err
	}
	model, effort, err := resolvePlanModeSettings(ctx, a, client, preset, sess)
	if err != nil {
		return nil, err
	}
	mode := &conversation.SessionCollaborationMode{
		Mode:  "plan",
		Model: model,
	}
	if preset != nil && preset.ReasoningEffort != nil {
		mode.PresetReasoningEffort = strings.TrimSpace(*preset.ReasoningEffort)
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return NormalizeThreadCollaborationMode(mode), nil
}

func PlanModeForSession(a Dependencies, sessionKey string) *conversation.SessionCollaborationMode {
	if a.ConfigProvider == nil || strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil {
		return nil
	}
	return NormalizeThreadCollaborationMode(sess.ActiveThreadCollaborationMode)
}

func sessionActiveThreadIDForLog(sess *conversation.Session) string {
	if sess == nil {
		return ""
	}
	return strings.TrimSpace(sess.ActiveThreadID)
}

func sessionActiveCollaborationModeForLog(sess *conversation.Session) *conversation.SessionCollaborationMode {
	if sess == nil {
		return nil
	}
	return sess.ActiveThreadCollaborationMode
}

func sessionBackendCollaborationModeForLog(sess *conversation.Session, backend string) *conversation.SessionCollaborationMode {
	if sess == nil || len(sess.BackendThreads) == 0 {
		return nil
	}
	snapshot, ok := sess.BackendThreads[strings.TrimSpace(backend)]
	if !ok {
		return nil
	}
	return snapshot.CollaborationMode
}

func ResolveDefaultCodexCollaborationModeForSession(a Dependencies, sess *conversation.Session) (*conversation.SessionCollaborationMode, error) {
	if mode := defaultCodexCollaborationModeForSession(a, sess); mode != nil {
		return mode, nil
	}
	if a.ConfigProvider == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	client, err := a.CodexClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(appcore.Context(a), 20*time.Second)
	defer cancel()
	model, effort, err := resolveDefaultCollaborationModeSettings(ctx, a, client)
	if err != nil {
		return nil, fmt.Errorf("无法解析 default collaboration mode model: %w", err)
	}
	mode := &conversation.SessionCollaborationMode{
		Mode:  "default",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return NormalizeThreadCollaborationMode(mode), nil
}

func defaultCodexCollaborationModeForSession(a Dependencies, sess *conversation.Session) *conversation.SessionCollaborationMode {
	model := ""
	effort := ""
	if a.ConfigProvider != nil && a.Config() != nil {
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
	mode := &conversation.SessionCollaborationMode{
		Mode:  "default",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return NormalizeThreadCollaborationMode(mode)
}

func canReuseCollaborationModeModelForDefault(a Dependencies, mode *conversation.SessionCollaborationMode) bool {
	mode = NormalizeThreadCollaborationMode(mode)
	if mode == nil {
		return false
	}
	if !strings.EqualFold(mode.Mode, "plan") {
		return true
	}
	if a.ConfigProvider == nil || a.Config() == nil {
		return true
	}
	return strings.TrimSpace(modelconfig.ConfiguredPlanModel(a.Config())) == ""
}

func PlanModeTitleForSession(a Dependencies, sessionKey, title string) string {
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

func ContentCardTitleForSubmission(a Dependencies, sub *domainsubmission.Submission, title string) string {
	if sub == nil {
		return strings.TrimSpace(title)
	}
	return ContentCardTitleForSession(a, sub.SessionKey, sub.WorkspaceID, title)
}

func ContentCardTitleForSession(a Dependencies, sessionKey, workspaceID, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if strings.TrimSpace(workspaceID) == "" && a.ConfigProvider != nil && sessionKey != "" {
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

func sessionWorkspaceTitleForSession(a Dependencies, sessionKey, title string) string {
	title = strings.TrimSpace(title)
	if title == "" || a.ConfigProvider == nil || strings.TrimSpace(sessionKey) == "" {
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

func resolvePlanModeSettings(ctx context.Context, a Dependencies, client CodexClient, preset *catalog.CollaborationModeMask, sess *conversation.Session) (model string, effort string, err error) {
	model, effort = a.EffectivePlanSettings(sess)
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(modelconfig.ConfiguredGlobalModel(a.Config()))
	}
	if effort == "" {
		effort = strings.TrimSpace(modelconfig.ConfiguredPlanReasoningEffort(a.Config()))
	}
	if effort == "" && preset != nil && preset.ReasoningEffort != nil {
		effort = strings.TrimSpace(*preset.ReasoningEffort)
	}
	if model != "" {
		return model, effort, nil
	}
	result, err := client.ListModels(ctx, 20)
	if err != nil {
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

func resolveDefaultCollaborationModeSettings(ctx context.Context, a Dependencies, client CodexClient) (model string, effort string, err error) {
	model = strings.TrimSpace(modelconfig.ConfiguredGlobalModel(a.Config()))
	effort = strings.TrimSpace(modelconfig.ConfiguredGlobalReasoningEffort(a.Config()))
	if model != "" {
		return model, effort, nil
	}
	result, err := client.ListModels(ctx, 20)
	if err != nil {
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

func StateForTurnStart(a Dependencies, sessionKey, threadID string) *conversation.SessionCollaborationMode {
	if a.ConfigProvider == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil
	}
	return DefaultCollaborationModeWithConfiguredEffort(a, sess.ActiveThreadCollaborationMode)
}

func DefaultCollaborationModeWithConfiguredEffort(a Dependencies, mode *conversation.SessionCollaborationMode) *conversation.SessionCollaborationMode {
	mode = NormalizeThreadCollaborationMode(mode)
	if mode == nil || !strings.EqualFold(mode.Mode, "default") || strings.TrimSpace(mode.ReasoningEffort) != "" {
		return mode
	}
	if a.ConfigProvider == nil || a.Config() == nil {
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

func NormalizeThreadCollaborationMode(mode *conversation.SessionCollaborationMode) *conversation.SessionCollaborationMode {
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

func (d Dependencies) WorkspaceSelection() workspace.SelectionService {
	if d.ConfigProvider == nil {
		return workspace.SelectionService{}
	}
	return d.ConfigProvider.WorkspaceSelection()
}
