package feishuapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/adapter/feishu/claudesupport"
	"feidex/internal/application"
	"feidex/internal/application/inbound"
	appinteraction "feidex/internal/application/interaction"
	approuting "feidex/internal/application/routing"
	"feidex/internal/application/submission"
	"feidex/internal/config"
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

type inboundRootInputs struct {
	pendingTextRequest       func(string, string) *interaction.PendingRequest
	completeWorkspaceNewText func(*application.InboundMessage, *interaction.PendingRequest) error
	completePlanModeText     func(*application.InboundMessage, *interaction.PendingRequest) error
}

func (p inboundRootInputs) PendingTextRequest(key, userID string) *interaction.PendingRequest {
	return p.pendingTextRequest(key, userID)
}
func (p inboundRootInputs) HandlePendingTextResponse(msg *application.InboundMessage, pending *interaction.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	switch pending.Kind {
	case "workspace_new":
		return p.completeWorkspaceNewText(msg, pending)
	case "claude_exit_plan_mode":
		return p.completePlanModeText(msg, pending)
	default:
		return nil
	}
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
	sessionKey    func(*application.InboundMessage) string
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
	sessionKey := p.sessionKey(msg)
	result, err := p.pending.Gate(msg, sessionKey, primary, time.Now().Unix())
	if err != nil || !result.Handled {
		return result.Handled, err
	}
	if err := p.runner.Run(p.context(), result.Effects); err != nil {
		return false, err
	}
	card := p.workspaceMenu(sessionKey)
	_, err = p.outbound.ReplyCard(p.context(), msg.MessageID, card, false)
	return true, err
}

type inboundCommands struct {
	backend func() string
	handle  func(*application.InboundMessage, string) error
}

func (p inboundCommands) IsLocalCommand(msg *application.InboundMessage, text string) bool {
	return isLocalCommandForMessage(p.backend(), msg, text)
}
func (p inboundCommands) HandleCommand(msg *application.InboundMessage, text string) error {
	return p.handle(msg, text)
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

type InboundPortInputs struct {
	FrontendID            string
	Context               func() context.Context
	SessionKey            func(*application.InboundMessage) string
	Feishu                FeishuClient
	Primary               approuting.Service
	PrimaryInitialization approuting.InitializationService
	GroupMessages         approuting.GroupMessages
	Requests              inbound.Requests
	InteractionLifecycle  appinteraction.LifecycleService
	CompleteWorkspaceText func(*application.InboundMessage, *interaction.PendingRequest) error
	ClaudeSupport         *claudesupport.Service
	Continuation          inbound.Continuation
	PendingQueue          *submission.PendingQueueService
	Config                *config.Config
	BindingPending        approuting.PendingService
	StoreReady            bool
	StateReady            bool
	GateContext           func() context.Context
	Effects               backendruntime.EffectRunner
	WorkspaceMenu         func(string) map[string]any
	LocalBackend          func() string
	HandleCommand         func(*application.InboundMessage, string) error
	SelectBackend         func(*application.InboundMessage, string) error
	BlockedReason         func() string
	RuntimeDeps           BackendRuntimeDeps
	Queue                 inbound.SubmissionQueue
	RefreshGroup          func(string, string)
	FlushNotifications    func(*application.InboundMessage)
	PrefetchForward       func(*application.InboundMessage)
}

// InboundPorts assembles the inbound adapters from already-composed frontend
// services. Keeping the inputs explicit prevents this graph from retaining
// the frontend aggregate through its command adapter.
func InboundPorts(inputs InboundPortInputs) inbound.Dependencies {
	return inbound.Dependencies{
		FrontendID: inputs.FrontendID, Context: inputs.Context, SessionKey: inputs.SessionKey,
		Routing: inboundRouting{
			frontendID: inputs.FrontendID, feishu: inputs.Feishu, primary: inputs.Primary,
			primaryInitialization: inputs.PrimaryInitialization, groupMessages: inputs.GroupMessages,
		}, Requests: inputs.Requests, RootInputs: inboundRootInputs{
			pendingTextRequest: func(key, userID string) *interaction.PendingRequest {
				return rootPendingTextRequest(inputs.InteractionLifecycle, key, userID)
			},
			completeWorkspaceNewText: inputs.CompleteWorkspaceText,
			completePlanModeText: func(msg *application.InboundMessage, pending *interaction.PendingRequest) error {
				return completeClaudePlanModeText(inputs.ClaudeSupport, msg, pending)
			},
		},
		Continuation: inputs.Continuation, Pending: inboundPending{PendingQueueService: inputs.PendingQueue, attachments: func(msg *application.InboundMessage, workspaceID, key string) ([]domainsubmission.SubmissionAttachment, error) {
			return resolveInboundAttachments(inputs.Config, inputs.Context, inputs.Feishu, msg, workspaceID, key)
		}},
		Bindings: inboundBindings{
			gate: pendingGroupMessageGate{
				pending: inputs.BindingPending, primary: inputs.Primary, frontendID: inputs.FrontendID,
				storeReady: inputs.StoreReady, sessionKey: inputs.SessionKey, context: inputs.GateContext,
				runner: inputs.Effects, workspaceMenu: inputs.WorkspaceMenu,
				outbound: newEffectOutbound(inputs.FrontendID, inputs.Effects),
			},
			pending: inputs.BindingPending, stateReady: inputs.StateReady,
		}, Commands: inboundCommands{backend: inputs.LocalBackend, handle: inputs.HandleCommand}, Backend: inboundBackend{
			configured: inputs.LocalBackend, selectBackend: inputs.SelectBackend,
			blockedReason: inputs.BlockedReason, runtimeDeps: inputs.RuntimeDeps,
		}, Queue: inputs.Queue, RefreshGroup: inputs.RefreshGroup,
		FlushNotifications: inputs.FlushNotifications, PrefetchForward: inputs.PrefetchForward,
	}
}

func (p inboundPending) StageInboundImages(msg *application.InboundMessage, key string) error {
	return p.StageInboundImagesForSession(msg, strings.TrimSpace(key), p.attachments)
}
