package feishuapp

import (
	"context"
	"encoding/json"
	"feidex/internal/application"
	"feidex/internal/application/backendops"
	"feidex/internal/application/interaction"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domainsubmission "feidex/internal/domain/submission"
	apputil "feidex/internal/formatutil"
	"log/slog"
	"strings"

	appreview "feidex/internal/adapter/feishu/review"

	"feidex/internal/adapter/backend/interactionreply"
	"feidex/internal/adapter/feishu/serverrequest"
	"feidex/internal/feishu"
	appruntime "feidex/internal/runtime"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) ServerRequestService() *serverrequest.Service {
	if a == nil {
		return nil
	}
	return a.bindings.ServerRequests
}

func BuildServerRequests(a *App) *serverrequest.Service {
	// Read once at construction so the dependency is visible.
	pendingReplies := a.bindings.PendingReplies
	submissionLookup := a.bindings.SubmissionLookup

	if a == nil {
		return nil
	}
	service := &serverrequest.Service{
		// State access
		PendingRequests: func() []*state.PendingRequest { return a.State().PendingRequests() },
		Pending:         func(id string) *state.PendingRequest { return a.State().Pending(id) },
		Submission:      func(id string) *domainsubmission.Submission { return a.State().Submission(id) },
		Session:         func(key string) *conversation.Session { return a.State().Session(key) },
		SessionKeysEqual: func(left, right string) bool {
			return sessionKeysEqual(left, right)
		},

		// Feishu
		SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
			if a.feishu == nil {
				return nil
			}
			return a.feishu.SimpleStatusCard(title, color, body, buttons)
		},
		PatchCard: func(messageID string, card map[string]any) error {
			if a.feishu == nil {
				return nil
			}
			return patchCardEffect(context.Background(), a, messageID, card)
		},
		ContentCardTitle: func(sessionKey, workspaceID, title string) string {
			return contentCardTitleForSession(a, sessionKey, workspaceID, title)
		},

		// Backend adapter factory
		AdapterForPending: func(pending *state.PendingRequest) serverrequest.BackendAdapter {
			backend := pendingBackend(a.configView(), pending)
			switch normalizeRuntimeBackend(backend) {
			case domainbackend.BackendCodex:
				client := a.runtimeView().currentCodexClient()
				if client == nil {
					return interactionreply.NewUnsupportedAdapter(backend)
				}
				return interactionreply.NewCodexAdapter(codexEffectReplyClient{app: a}, backend)
			case domainbackend.BackendClaude:
				if a.runtimeView().currentClaudeCore() == nil {
					return interactionreply.NewUnsupportedAdapter(backend)
				}
				return interactionreply.NewClaudeAdapter(claudeReplyClientShim{claude: a.runtimeView().currentClaudeCore()}, backend)
			default:
				return interactionreply.NewUnsupportedAdapter(backend)
			}
		},

		// Root service delegation
		FinalizePendingReply: func(pending *state.PendingRequest) *state.PendingRequest {
			return pendingReplies.Finalize(pending)
		},
		FindSubmissionByTurn: func(threadID, turnID string) (string, *domainsubmission.Submission) {
			return submissionLookup.FindSubmissionByTurn(threadID, turnID)
		},
		DeliverPendingCard: func(sub *domainsubmission.Submission, card map[string]any, delivery serverrequest.PendingCardDelivery) error {
			return deliverPendingCard(a, sub, card, pendingCardDelivery{
				requestKey:      delivery.RequestKey,
				requestIDStored: delivery.RequestIDStored,
				backend:         delivery.Backend,
				kind:            delivery.Kind,
				sessionKey:      delivery.SessionKey,
				threadID:        delivery.ThreadID,
				turnID:          delivery.TurnID,
				itemID:          delivery.ItemID,
				ownerUserID:     delivery.OwnerUserID,
				payloadJSON:     delivery.PayloadJSON,
				waitingStatus:   delivery.WaitingStatus,
				linkKind:        delivery.LinkKind,
				ttl:             delivery.TTL,
			})
		},
		RenderApprovalCard: func(sub *domainsubmission.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
			return renderApprovalCard(a.State(), a.feishu, sub, title, color, body, buttons)
		},
		PrepareMentionText: func(text, userID string) string {
			return apputil.PrependAttentionMentionMarkdown(text, userID)
		},
		ReplyCodexError: func(requestID json.RawMessage, code int, message string) {
			replyCodexError(a.runtimeView(), requestID, code, message)
		},
		RawCard: rawCard,

		// Constants
		BackendCodex:  domainbackend.BackendCodex,
		BackendClaude: domainbackend.BackendClaude,
	}
	return service
}

// completePendingFormCancelDispatch routes pending_form.cancel to either
// serverrequest (for server-resolved kinds) or root (for workspace_*/review_form/claude_exit_plan_mode).
func completePendingFormCancelDispatch(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.State().Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	switch pending.Kind {
	case "workspace_new", "workspace_clone", "workspace_worktree", "review_form", "claude_exit_plan_mode":
		return completeRootPendingFormCancel(a, pending)
	default:
		return a.ServerRequestService().CompletePendingFormCancel(action)
	}
}

// completeRootPendingFormCancel handles cancel for kinds whose logic stays in root.
func completeRootPendingFormCancel(a *App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, error) {
	// claude_exit_plan_mode needs backend cancel via the Claude adapter.
	if pending.Kind == "claude_exit_plan_mode" {
		adapter := a.ServerRequestService().AdapterForPending(pending)
		if err := adapter.CancelPending(pending); err != nil {
			slog.Error("root cancel backend reply failed", "kind", pending.Kind, "request_id", pending.ID, "error", err)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "取消提交失败，请重试"},
			}, nil
		}
	}
	a.bindings.PendingReplies.Finalize(pending)
	switch pending.Kind {
	case "workspace_new", "workspace_clone", "workspace_worktree":
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已返回工作区"},
			Card:  rawCard(a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(pending.SessionKey)),
		}, nil
	case "review_form":
		body := reviewCancelledBody(pending)
		if body == "" {
			body = "该请求已取消。"
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消"},
			Card:  rawCard(a.feishu.SimpleStatusCard("Review 已取消", "grey", body, nil)),
		}, nil
	case "claude_exit_plan_mode":
		body := claudePlanCancelledBody(pending)
		if body == "" {
			body = "该请求已取消。"
		}
		title := contentCardTitleForSession(a, pending.SessionKey, "", "计划确认已取消")
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消"},
			Card:  rawCard(a.feishu.SimpleStatusCard(title, "grey", body, nil)),
		}, nil
	default:
		slog.Warn("completeRootPendingFormCancel: unhandled kind", "kind", pending.Kind)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消"},
		}, nil
	}
}

// rootPendingTextRequest finds the most recent open pending text request
// for kinds whose logic stays in root (workspace_new, claude_exit_plan_mode).
func rootPendingTextRequest(interactionlifecycle interaction.LifecycleService, sessionKey, userID string) *state.PendingRequest {
	return interactionlifecycle.LatestTextRequest(sessionKey, userID, "workspace_new", "claude_exit_plan_mode")
}

// claudeReplyClientShim adapts ClaudeCore to serverrequest.ClaudeReplyClient.
type claudeReplyClientShim struct {
	claude ClaudeCore
}

func (s claudeReplyClientShim) ResolveApproval(id string, resolution appruntime.ClaudeApprovalResolution) error {
	return s.claude.ResolveApproval(id, resolution)
}

func (s claudeReplyClientShim) ResolveUserInput(id string, answers map[string]string) error {
	return s.claude.ResolveUserInput(id, answers)
}

func (s claudeReplyClientShim) CancelPending(id, reason string) error {
	return s.claude.CancelPending(id, reason)
}

// reviewCancelledBody renders the cancelled body text for review pending requests.
func reviewCancelledBody(pending *state.PendingRequest) string {
	payload := reviewPendingPayloadFromPending(pending)
	lines := []string{"已取消本次 review 请求。", "", "原请求："}
	switch payload.Mode {
	case reviewFormModeBase:
		lines = append(lines, "模式: base branch")
		if branch := strings.TrimSpace(payload.Branch); branch != "" {
			lines = append(lines, "当前选择: `"+branch+"`")
		}
	case reviewFormModeCommit:
		lines = append(lines, "模式: commit")
		if sha := strings.TrimSpace(payload.CommitSHA); sha != "" {
			lines = append(lines, "当前选择: `"+appreview.ShortCommitSHA(sha)+"`")
		}
		if title := strings.TrimSpace(payload.CommitTitle); title != "" {
			lines = append(lines, title)
		}
	case reviewFormModeCustom:
		lines = append(lines, "模式: custom")
		if instructions := strings.TrimSpace(payload.Instructions); instructions != "" {
			lines = append(lines, "Instructions:", instructions)
		}
	default:
		return ""
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type codexEffectReplyClient struct{ app *App }

func (c codexEffectReplyClient) Reply(token json.RawMessage, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return newEffectRunner(c.app.runtimeOwner).Run(c.app.Context(), []application.Effect{application.ResolveBackendRequest{Frontend: identity.FrontendID(c.app.FrontendID()), Backend: domainbackend.BackendCodex, Response: backendops.Response{Token: append([]byte(nil), token...), Payload: encoded}}})
}
func (c codexEffectReplyClient) ReplyError(token json.RawMessage, code int, message string) error {
	return newEffectRunner(c.app.runtimeOwner).Run(c.app.Context(), []application.Effect{application.ResolveBackendRequest{Frontend: identity.FrontendID(c.app.FrontendID()), Backend: domainbackend.BackendCodex, Response: backendops.Response{Token: append([]byte(nil), token...), Error: &backendops.ResponseError{Code: code, Message: message}}}})
}
