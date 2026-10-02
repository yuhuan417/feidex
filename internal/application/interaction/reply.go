package interaction

import domain "feidex/internal/domain/interaction"

// BackendReply is the interface for backend-specific pending request replies.
type BackendReply interface {
	Kind() string
	ReplyApproval(pending *domain.PendingRequest, actionName string, replyPayload any) error
	ReplyQuickUserInput(pending *domain.PendingRequest, payload domain.ToolUserInputPayload, questionID, answer string) (string, error)
	ReplyFormUserInput(pending *domain.PendingRequest, payload domain.ToolUserInputPayload, selections map[string]string) (string, error)
	ReplyTextUserInput(pending *domain.PendingRequest, payload domain.ToolUserInputPayload, text string) (string, error)
	ReplyElicitationAction(pending *domain.PendingRequest, action string) error
	ReplyElicitationContent(pending *domain.PendingRequest, content map[string]any) error
	ReplyElicitationForm(pending *domain.PendingRequest, payload domain.ElicitationFormPayload, text string) (string, error)
	ReplyElicitationURL(pending *domain.PendingRequest, actionName string) (string, error)
	CancelPending(pending *domain.PendingRequest) error
}
