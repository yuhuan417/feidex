package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	appreview "feidex/internal/app/review"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/app/serverrequest"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ServerRequestService returns the serverrequest.Service for this app.
func (a *App) ServerRequestService() *serverrequest.Service {
	if a.serverRequestSvc != nil {
		return a.serverRequestSvc
	}
	a.serverRequestSvc = &serverrequest.Service{
		// State access
		PendingRequests: func() []*state.PendingRequest { return a.State().PendingRequests() },
		Pending:         func(id string) *state.PendingRequest { return a.State().Pending(id) },
		UpdatePending:   func(id string, mutate func(*state.PendingRequest)) error { return a.State().UpdatePending(id, mutate) },
		SavePending:     func(req *state.PendingRequest) error { return a.State().SavePending(req) },
		SetSubStatus:    func(id, status string) error { return a.State().SetSubmissionStatus(id, status) },
		Submission:      func(id string) *state.Submission { return a.State().Submission(id) },
		Session:         func(key string) *state.Session { return a.State().Session(key) },

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
			return a.feishu.PatchCard(context.Background(), messageID, card)
		},

		// Backend adapter factory
		AdapterForPending: func(pending *state.PendingRequest) serverrequest.BackendAdapter {
			backend := pendingBackend(a, pending)
			switch normalizeRuntimeBackend(backend) {
			case backendCodex:
				client := currentCodexClient(a)
				if client == nil {
					return serverrequest.NewUnsupportedAdapter(backend)
				}
				return serverrequest.NewCodexAdapter(client, backend)
			case backendClaude:
				if a.claude == nil {
					return serverrequest.NewUnsupportedAdapter(backend)
				}
				return serverrequest.NewClaudeAdapter(claudeReplyClientShim{claude: a.claude}, backend)
			default:
				return serverrequest.NewUnsupportedAdapter(backend)
			}
		},

		// Root service delegation
		FinalizePendingReply: func(pending *state.PendingRequest) *state.PendingRequest {
			return newRuntimeStateService(a).finalizePendingReply(pending)
		},
		HasOpenPendingRequestForTurn: func(threadID, turnID, excludeID string) bool {
			return newRuntimeStateService(a).hasOpenPendingRequestForTurn(threadID, turnID, excludeID)
		},
		FindSubmissionByTurn: func(threadID, turnID string) (string, *state.Submission) {
			return newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn(threadID, turnID)
		},
		DeliverPendingCard: func(sub *state.Submission, card map[string]any, delivery serverrequest.PendingCardDelivery) error {
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
		RenderApprovalCard: func(sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
			return renderApprovalCard(a, "", sub, title, color, body, buttons)
		},
		PrepareMentionText: func(text, userID string) string {
			return prependAttentionMentionMarkdown(text, userID)
		},
		ReplyCodexError: func(requestID json.RawMessage, code int, message string) {
			replyCodexError(a, requestID, code, message)
		},
		RawCard: rawCard,

		// Constants
		BackendCodex:  backendCodex,
		BackendClaude: backendClaude,
	}
	return a.serverRequestSvc
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
	case "workspace_new", "workspace_clone", "review_form", "claude_exit_plan_mode":
		return completeRootPendingFormCancel(a, pending)
	default:
		return a.ServerRequestService().CompletePendingFormCancel(action)
	}
}

// completeRootPendingFormCancel handles cancel for kinds whose logic stays in root.
func completeRootPendingFormCancel(a *App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, error) {
	// claude_exit_plan_mode needs backend cancel via the Claude adapter.
	if pending.Kind == claudePlanModePendingKind {
		adapter := a.ServerRequestService().AdapterForPending(pending)
		if err := adapter.CancelPending(pending); err != nil {
			slog.Error("root cancel backend reply failed", "kind", pending.Kind, "request_id", pending.ID, "error", err)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "取消提交失败，请重试"},
			}, nil
		}
	}
	newRuntimeStateService(a).finalizePendingReply(pending)
	switch pending.Kind {
	case "workspace_new", "workspace_clone":
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已返回工作区"},
			Card:  rawCard(newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(pending.SessionKey)),
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
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消"},
			Card:  rawCard(a.feishu.SimpleStatusCard("计划确认已取消", "grey", body, nil)),
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
func rootPendingTextRequest(a *App, sessionKey, userID string) *state.PendingRequest {
	var best *state.PendingRequest
	for _, req := range a.State().PendingRequests() {
		if req == nil || state.NormalizePendingRequestStatus(req.Status) != state.PendingRequestStatusPending || req.SessionKey != sessionKey {
			continue
		}
		if req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		switch req.Kind {
		case "workspace_new", "claude_exit_plan_mode":
			if best == nil || req.CreatedAt > best.CreatedAt {
				best = req
			}
		}
	}
	return best
}

// handleRootPendingTextResponse dispatches a text reply for root-owned pending kinds.
func handleRootPendingTextResponse(a *App, msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	svc := newPendingInputService(a)
	switch pending.Kind {
	case "workspace_new":
		return newWorkspaceManagementServiceInner(a).CompleteWorkspaceNewText(msg, pending)
	case claudePlanModePendingKind:
		return svc.completeClaudePlanModeText(msg, pending)
	default:
		return nil
	}
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
