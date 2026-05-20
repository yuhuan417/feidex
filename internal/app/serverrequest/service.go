package serverrequest

import (
	"encoding/json"

	"feidex/internal/app/approvalview"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Service manages pending request operations with callbacks for app/
// dependencies. Follows the claudesupport.Service pattern.
type Service struct {
	// State access
	PendingRequests func() []*state.PendingRequest
	Pending         func(id string) *state.PendingRequest
	UpdatePending   func(id string, mutate func(*state.PendingRequest)) error
	SavePending     func(req *state.PendingRequest) error
	SetSubStatus    func(id, status string) error
	Submission      func(id string) *state.Submission
	Session         func(key string) *state.Session

	// Feishu
	SimpleStatusCard func(title, color, body string, buttons []feishu.Button) map[string]any
	PatchCard        func(messageID string, card map[string]any) error
	ContentCardTitle func(sessionKey, workspaceID, title string) string

	// Backend adapter factory
	AdapterForPending func(pending *state.PendingRequest) BackendAdapter

	// Root service delegation
	FinalizePendingReply         func(pending *state.PendingRequest) *state.PendingRequest
	HasOpenPendingRequestForTurn func(threadID, turnID, excludeID string) bool
	FindSubmissionByTurn         func(threadID, turnID string) (string, *state.Submission)
	DeliverPendingCard           func(sub *state.Submission, card map[string]any, delivery PendingCardDelivery) error
	RenderApprovalCard           func(sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any
	PrepareMentionText           func(text, userID string) string
	ReplyCodexError              func(requestID json.RawMessage, code int, message string)
	RawCard                      func(card map[string]any) *callback.Card

	// Constants
	BackendCodex  string
	BackendClaude string
}

// BackendAdapter is the interface for backend-specific pending request replies.
type BackendAdapter interface {
	Kind() string
	ReplyApproval(pending *state.PendingRequest, actionName string, replyPayload any) error
	ReplyQuickUserInput(pending *state.PendingRequest, payload ToolUserInputPayload, questionID, answer string) (string, error)
	ReplyFormUserInput(pending *state.PendingRequest, payload ToolUserInputPayload, selections map[string]string) (string, error)
	ReplyTextUserInput(pending *state.PendingRequest, payload ToolUserInputPayload, text string) (string, error)
	ReplyElicitationAction(pending *state.PendingRequest, action string) error
	ReplyElicitationContent(pending *state.PendingRequest, content map[string]any) error
	ReplyElicitationForm(pending *state.PendingRequest, payload ElicitationFormPayload, text string) (string, error)
	ReplyElicitationURL(pending *state.PendingRequest, actionName string) (string, error)
	CancelPending(pending *state.PendingRequest) error
}

// Approval view helpers — direct imports, no wrapper vars.
var (
	approvalDecisionText   = approvalview.ApprovalDecisionText
	approvalDecisionDetail = approvalview.ApprovalDecisionDetail
	approvalDecisionColor  = approvalview.ApprovalDecisionColor
	approvalBodyText       = approvalview.ApprovalBodyText
	approvalRequestPayload = approvalview.ApprovalRequestPayload
)
