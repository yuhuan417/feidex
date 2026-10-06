package planmode

import (
	"context"
	planapp "feidex/internal/application/plan"
	"feidex/internal/application/workspace"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	catalog "feidex/internal/domain/modelconfig"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"sync"

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
	UseCase        *planapp.Service
	ConfigProvider interface {
		Config() *config.Config
		ConfigMu() *sync.RWMutex
		Backend() string
		FrontendID() string
		FrontendConfigIndex() int
		Store() *state.Store
		WorkspaceSelection() workspace.SelectionService
	}
	ContextProvider              interface{ Context() context.Context }
	StateProvider                StateProvider
	Outbound                     Outbound
	CardRenderer                 CardRenderer
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
	StartWorkspaceThreadFn       func(string, *conversation.Session, *config.Workspace) (*conversation.ThreadBinding, error)
}

type Outbound interface {
	ReplyInteractionCard(context.Context, string, string, map[string]any, bool) (string, error)
	ReplyText(context.Context, string, string, bool) error
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	PatchCard(context.Context, string, map[string]any) error
}

type CardRenderer interface {
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
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
func (d Dependencies) State() StateProvider         { return d.StateProvider }
func (d Dependencies) OutboundCapability() Outbound { return d.Outbound }
func (d Dependencies) Renderer() CardRenderer       { return d.CardRenderer }
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
	d.RunAsyncFn(fn)
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
func (d Dependencies) StartWorkspaceThread(k string, s *conversation.Session, w *config.Workspace) (*conversation.ThreadBinding, error) {
	if d.StartWorkspaceThreadFn == nil {
		return nil, fmt.Errorf("workspace thread starter unavailable")
	}
	return d.StartWorkspaceThreadFn(k, s, w)
}

type StateProvider interface {
	Session(key string) *conversation.Session
	Pending(id string) *state.PendingRequest
	PendingRequests() []*state.PendingRequest
	QueueSubmission(sessionKey, submissionID string) error
	Submission(id string) *domainsubmission.Submission
}

type SessionStateProvider interface {
	Session(key string) *conversation.Session
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
	option := ""
	if len(args) > 0 {
		option = args[0]
	}
	result, err := a.UseCase.Configure(sessionKey, option)
	if err != nil {
		return err
	}
	reason := "当前 thread 已关闭 plan mode，旧的计划确认已失效。"
	text := "当前 thread 已关闭 `plan` collaboration mode。"
	if result.Enabled {
		reason = "当前 thread 已重新配置 plan mode，旧的计划确认已失效。"
		text = RenderPlanModeStatusText(result.Mode)
	}
	InvalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, reason)
	return a.OutboundCapability().ReplyText(a.Context(), msg.MessageID, text, a.ReplyInThreadEnabled(msg.ChatType))
}

func RenderPlanModeStatusText(mode *conversation.SessionCollaborationMode) string {
	mode = conversation.NormalizeCollaborationMode(mode)
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

func PlanModeForSession(a Dependencies, sessionKey string) *conversation.SessionCollaborationMode {
	return PlanModeForSessionFromState(a.StateProvider, a.ConfigProvider != nil, sessionKey)
}

func PlanModeForSessionFromState(state SessionStateProvider, enabled bool, sessionKey string) *conversation.SessionCollaborationMode {
	if !enabled || state == nil || strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	sess := state.Session(sessionKey)
	if sess == nil {
		return nil
	}
	return conversation.NormalizeCollaborationMode(sess.ActiveThreadCollaborationMode)
}

func PlanModeTitleForSession(a Dependencies, sessionKey, title string) string {
	return PlanModeTitleForSessionFromState(a.StateProvider, a.ConfigProvider != nil, sessionKey, title)
}

func PlanModeTitleForSessionFromState(state SessionStateProvider, enabled bool, sessionKey, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	title = ContentCardTitleForSessionFromState(state, enabled, sessionKey, "", title)
	mode := PlanModeForSessionFromState(state, enabled, sessionKey)
	if mode == nil || !strings.EqualFold(mode.Mode, "plan") {
		return title
	}
	return prependTitlePrefix(title, "[plan]")
}

func ContentCardTitleForSubmission(a Dependencies, sub *domainsubmission.Submission, title string) string {
	return ContentCardTitleForSubmissionFromState(a.StateProvider, a.ConfigProvider != nil, sub, title)
}

// ContentCardTitleForSubmissionFromState projects session presentation state
// without requiring the frontend configuration aggregate. Callers that have
// already established a live frontend can pass its scoped state provider.
func ContentCardTitleForSubmissionFromState(state SessionStateProvider, enabled bool, sub *domainsubmission.Submission, title string) string {
	if sub == nil {
		return strings.TrimSpace(title)
	}
	return ContentCardTitleForSessionFromState(state, enabled, sub.SessionKey, sub.WorkspaceID, title)
}

func ContentCardTitleForSession(a Dependencies, sessionKey, workspaceID, title string) string {
	return ContentCardTitleForSessionFromState(a.StateProvider, a.ConfigProvider != nil, sessionKey, workspaceID, title)
}

// ContentCardTitleForSessionFromState renders workspace and plan-mode prefixes
// from the current scoped session snapshot.
func ContentCardTitleForSessionFromState(state SessionStateProvider, enabled bool, sessionKey, workspaceID, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if strings.TrimSpace(workspaceID) == "" && enabled && state != nil && sessionKey != "" {
		if sess := state.Session(sessionKey); sess != nil {
			workspaceID = strings.TrimSpace(sess.WorkspaceID)
		}
	}
	planMode := false
	if mode := planModeForState(state, enabled, sessionKey); mode != nil {
		planMode = strings.EqualFold(mode.Mode, "plan")
	}
	return normalizeContentCardTitle(title, workspaceID, planMode)
}

func planModeForState(state SessionStateProvider, enabled bool, sessionKey string) *conversation.SessionCollaborationMode {
	if !enabled || state == nil || strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	sess := state.Session(sessionKey)
	if sess == nil {
		return nil
	}
	return conversation.NormalizeCollaborationMode(sess.ActiveThreadCollaborationMode)
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

func (d Dependencies) WorkspaceSelection() workspace.SelectionService {
	if d.ConfigProvider == nil {
		return workspace.SelectionService{}
	}
	return d.ConfigProvider.WorkspaceSelection()
}

func firstNonEmpty(values ...string) string {
	return textutil.FirstNonEmpty(values...)
}
func configuredBackend(a Dependencies) string {
	if a.ConfigProvider == nil {
		return ""
	}
	if value := strings.TrimSpace(a.ConfigProvider.Backend()); value != "" {
		return domainbackend.NormalizeBackend(value)
	}
	cfg := a.ConfigProvider.Config()
	if cfg == nil {
		return ""
	}
	index := a.ConfigProvider.FrontendConfigIndex()
	if index >= 0 && index < len(cfg.Frontends) {
		return domainbackend.NormalizeBackend(cfg.Frontends[index].FeishuConfig.Backend)
	}
	return domainbackend.NormalizeBackend(cfg.Feishu.Backend)
}
