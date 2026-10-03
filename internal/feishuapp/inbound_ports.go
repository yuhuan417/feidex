package feishuapp

import (
	"context"
	"strings"

	"feidex/internal/application"
	"feidex/internal/application/inbound"
	"feidex/internal/application/submission"
	"feidex/internal/domain/interaction"
	domainrouting "feidex/internal/domain/routing"
	domainsubmission "feidex/internal/domain/submission"
)

type inboundRouting struct{ app *App }

func (p inboundRouting) NormalizeGroupMessage(msg *application.InboundMessage) bool {
	msg.MentionedSelf = messageMentionsCurrentBot(p.app, msg.MentionedOpenIDs, msg.MentionedSelf)
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
	_, err := ensureGroupPrimaryInitialized(ctx, p.app, kind, id)
	return err
}
func (p inboundRouting) SyncPrimary(msg *application.InboundMessage) (bool, error) {
	return syncGroupPrimaryAssignment(p.app, msg)
}
func (p inboundRouting) DropsGroupMessage(msg *application.InboundMessage) bool {
	return routerDropsGroupMessage(p.app, msg)
}

type inboundRootInputs struct{ app *App }

func (p inboundRootInputs) PendingTextRequest(key, userID string) *interaction.PendingRequest {
	return rootPendingTextRequest(p.app, key, userID)
}
func (p inboundRootInputs) HandlePendingTextResponse(msg *application.InboundMessage, pending *interaction.PendingRequest) error {
	return handleRootPendingTextResponse(p.app, msg, pending)
}

type inboundPending struct {
	*submission.PendingQueueService
	attachments func(*application.InboundMessage, string, string) ([]domainsubmission.SubmissionAttachment, error)
}

type inboundBindings struct{ app *App }

func (p inboundBindings) GatePendingGroupMessage(msg *application.InboundMessage) (bool, error) {
	return p.app.bindings.BindingCommands.gatePendingGroupMessage(msg)
}
func (p inboundBindings) DiscardPendingMessage(id string) bool {
	return discardPendingBindingMessageByID(p.app, id)
}

type inboundCommands struct{ app *App }

func (p inboundCommands) IsLocalCommand(msg *application.InboundMessage, text string) bool {
	return isLocalCommandForMessage(configuredBackend(p.app), msg, text)
}
func (p inboundCommands) HandleCommand(msg *application.InboundMessage, text string) error {
	return handleCommand(p.app, msg, text)
}

type inboundBackend struct{ app *App }

func (p inboundBackend) Configured() bool { return hasConfiguredBackend(p.app) }
func (p inboundBackend) ReplySelection(msg *application.InboundMessage) error {
	return p.app.bindings.BackendSelection.ReplyBackendSelectionCard(msg, "")
}
func (p inboundBackend) BlockedReason() string {
	return p.app.runtimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic()
}
func (p inboundBackend) CheckMaintenance() error {
	if owner := backendRuntime(p.app); owner != nil {
		return owner.MaintenanceBlocksCommand(backendRuntimeContextForApp(p.app), "")
	}
	return nil
}

func InboundPorts(a *App) inbound.Dependencies {
	return inbound.Dependencies{
		FrontendID: a.FrontendID(), Context: a.Context, SessionKey: func(msg *application.InboundMessage) string { return makeSessionKey(a, msg) },
		Routing: inboundRouting{app: a}, Requests: a.bindings.ServerRequests, RootInputs: inboundRootInputs{app: a},
		Continuation: a.bindings.Continuation, Pending: inboundPending{PendingQueueService: a.bindings.PendingQueue, attachments: func(msg *application.InboundMessage, workspaceID, key string) ([]domainsubmission.SubmissionAttachment, error) {
			return resolveInboundAttachments(a, msg, workspaceID, key)
		}},
		Bindings: inboundBindings{app: a}, Commands: inboundCommands{app: a}, Backend: inboundBackend{app: a}, Queue: a.bindings.Submissions,
		RefreshGroup:       func(chatID, reason string) { scheduleGroupAnnouncementStatusRefresh(a, chatID, reason) },
		FlushNotifications: func(msg *application.InboundMessage) { flushPendingFrontendCardNotifications(a, msg) },
		PrefetchForward:    func(msg *application.InboundMessage) { a.bindings.ForwardInputs.Start(msg) },
	}
}

func (p inboundPending) StageInboundImages(msg *application.InboundMessage, key string) error {
	return p.StageInboundImagesForSession(msg, strings.TrimSpace(key), p.attachments)
}
