package feishuapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/application"
	"feidex/internal/application/inbound"
	approuting "feidex/internal/application/routing"
	"feidex/internal/application/submission"
	"feidex/internal/domain/interaction"
	domainrouting "feidex/internal/domain/routing"
	domainsubmission "feidex/internal/domain/submission"
	backendruntime "feidex/internal/runtime"
)

type inboundRouting struct {
	frontendID            string
	feishu                FeishuClient
	primary               approuting.Service
	primaryInitialization approuting.InitializationService
	groupMessages         approuting.GroupMessages
}

func (p inboundRouting) NormalizeGroupMessage(msg *application.InboundMessage) bool {
	msg.MentionedSelf = messageMentionsCurrentBot(p.feishu, msg.MentionedOpenIDs, msg.MentionedSelf)
	if _, ok := groupPrimaryAssignmentFromMessage(msg); ok && domainrouting.ParseEmptyBotMention(msg.Text) {
		msg.Text = "/primary on"
	}
	if isGroupPrimaryControlMessage(msg) {
		if _, ok := groupPrimaryAssignmentFromMessage(msg); !ok {
			return false
		}
	}
	return true
}
func (p inboundRouting) EnsurePrimary(ctx context.Context, kind, id string) error {
	kind = strings.ToLower(strings.TrimSpace(kind))
	id = strings.TrimSpace(id)
	if kind != "group" || id == "" {
		return nil
	}
	if p.feishu == nil {
		return fmt.Errorf("feishu client not initialized")
	}
	_, err := p.primaryInitialization.Ensure(ctx, p.frontendID, kind, id)
	return err
}
func (p inboundRouting) SyncPrimary(msg *application.InboundMessage) (bool, error) {
	return syncGroupPrimaryAssignment(p.primary, p.frontendID, p.feishu, msg)
}
func (p inboundRouting) DropsGroupMessage(msg *application.InboundMessage) bool {
	return routerDropsGroupMessage(p.groupMessages, msg)
}

type inboundRootInputs struct{ app *App }

func (p inboundRootInputs) PendingTextRequest(key, userID string) *interaction.PendingRequest {
	return rootPendingTextRequest(p.app.bindings.InteractionLifecycle, key, userID)
}
func (p inboundRootInputs) HandlePendingTextResponse(msg *application.InboundMessage, pending *interaction.PendingRequest) error {
	return handleRootPendingTextResponse(p.app, msg, pending)
}

type inboundPending struct {
	*submission.PendingQueueService
	attachments func(*application.InboundMessage, string, string) ([]domainsubmission.SubmissionAttachment, error)
}

type inboundBindings struct {
	gate       pendingGroupMessageGate
	pending    approuting.PendingService
	stateReady bool
}

func (p inboundBindings) GatePendingGroupMessage(msg *application.InboundMessage) (bool, error) {
	return p.gate.Handle(msg)
}
func (p inboundBindings) DiscardPendingMessage(id string) bool {
	return discardPendingBindingMessage(p.pending, p.stateReady, id)
}

type pendingGroupMessageGate struct {
	pending       approuting.PendingService
	primary       approuting.Service
	frontendID    string
	storeReady    bool
	config        frontendConfigView
	context       func() context.Context
	runner        backendruntime.EffectRunner
	workspaceMenu func(string) map[string]any
	outbound      effectOutbound
}

func (p pendingGroupMessageGate) Handle(msg *application.InboundMessage) (bool, error) {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "group" || strings.TrimSpace(msg.ChatID) == "" {
		return false, nil
	}
	primary := false
	if p.storeReady {
		primary, _ = p.primary.IsPrimary(p.frontendID, msg.ChatType, msg.ChatID)
	}
	sessionKey := p.config.makeSessionKey(msg)
	result, err := p.pending.Gate(msg, sessionKey, primary, time.Now().Unix())
	if err != nil || !result.Handled {
		return result.Handled, err
	}
	if err := p.runner.Run(p.context(), result.Effects); err != nil {
		return false, err
	}
	card := p.workspaceMenu(sessionKey)
	_, err = p.outbound.ReplyCard(p.context(), msg.MessageID, card, p.config.replyInThreadEnabled())
	return true, err
}

type inboundCommands struct{ app *App }

func (p inboundCommands) IsLocalCommand(msg *application.InboundMessage, text string) bool {
	return isLocalCommandForMessage(p.app.configView().configuredBackend(), msg, text)
}
func (p inboundCommands) HandleCommand(msg *application.InboundMessage, text string) error {
	return handleCommand(p.app, msg, text)
}

type inboundBackend struct {
	configured    func() string
	selectBackend func(*application.InboundMessage, string) error
	blockedReason func() string
	runtimeDeps   BackendRuntimeDeps
}

func (p inboundBackend) Configured() bool { return strings.TrimSpace(p.configured()) != "" }
func (p inboundBackend) ReplySelection(msg *application.InboundMessage) error {
	return p.selectBackend(msg, "")
}
func (p inboundBackend) BlockedReason() string { return p.blockedReason() }
func (p inboundBackend) CheckMaintenance() error {
	if owner := backendruntime.BackendForKind(p.configured()); owner != nil {
		return owner.MaintenanceBlocksCommand(backendRuntimeContextForApp(p.runtimeDeps.currentBackend()), "")
	}
	return nil
}

// InboundPorts takes the forward prefetch entry point as a value rather
// than reaching for a.bindings.ForwardInputs.
func InboundPorts(a *App, prefetchForward func(*application.InboundMessage)) inbound.Dependencies {
	announcementRefresh := a.runtimeOwner.Announcements
	configuredBackend := ConfiguredBackendBuilder(a.cfg, a.ConfigMu(), a.runtimeOwner.Backend, a.frontendID, a.frontendConfigIndex)
	runtimeDeps := a.BackendRuntimeDeps()
	frontendID := a.FrontendID()
	runner := *a.runtimeOwner.EffectRunner
	configView := a.configView()
	return inbound.Dependencies{
		FrontendID: a.FrontendID(), Context: a.Context, SessionKey: func(msg *application.InboundMessage) string { return a.configView().makeSessionKey(msg) },
		Routing: inboundRouting{frontendID: a.FrontendID(), feishu: a.feishu, primary: a.bindings.Primary, primaryInitialization: a.bindings.PrimaryInitialization, groupMessages: a.bindings.GroupMessages}, Requests: a.bindings.ServerRequests, RootInputs: inboundRootInputs{app: a},
		Continuation: a.bindings.Continuation, Pending: inboundPending{PendingQueueService: a.bindings.PendingQueue, attachments: func(msg *application.InboundMessage, workspaceID, key string) ([]domainsubmission.SubmissionAttachment, error) {
			return resolveInboundAttachments(a.cfg, a.Context, a.feishu, msg, workspaceID, key)
		}},
		Bindings: inboundBindings{
			gate: pendingGroupMessageGate{
				pending: a.bindings.BindingPending, primary: a.bindings.Primary, frontendID: frontendID, storeReady: a.Store() != nil,
				config: configView, context: a.runtimeOwner.Lifecycle.Context, runner: runner,
				workspaceMenu: a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard,
				outbound:      newEffectOutbound(frontendID, runner),
			},
			pending: a.bindings.BindingPending, stateReady: a.State() != nil,
		}, Commands: inboundCommands{app: a}, Backend: inboundBackend{
			configured: configuredBackend, selectBackend: a.bindings.BackendSelection.ReplyBackendSelectionCard,
			blockedReason: a.runtimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic,
			runtimeDeps:   runtimeDeps,
		}, Queue: a.bindings.Submissions,
		RefreshGroup:       func(chatID, reason string) { scheduleGroupAnnouncementStatusRefresh(announcementRefresh, chatID) },
		FlushNotifications: func(msg *application.InboundMessage) { flushPendingFrontendCardNotifications(a, msg) },
		PrefetchForward:    prefetchForward,
	}
}

func (p inboundPending) StageInboundImages(msg *application.InboundMessage, key string) error {
	return p.StageInboundImagesForSession(msg, strings.TrimSpace(key), p.attachments)
}
