package feishuapp

import (
	"context"
	"encoding/json"

	"feidex/internal/adapter/feishu/approval"
	"feidex/internal/adapter/feishu/serverrequest"
	"feidex/internal/application"
	"feidex/internal/application/backendevents"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
)

type BackendInteractionPresenterPorts struct {
	FindSubmissionByTurn         func(string, string) (string, *domainsubmission.Submission)
	WorkspaceCwd                 func(string) string
	MergeApprovalPresentation    func(approval.Presentation) approval.Presentation
	SendApprovalCardPresentation func(json.RawMessage, approval.Presentation)
	SendUserInputCard            func(json.RawMessage, serverrequest.ToolUserInputPayload)
	SendUserInputFormCard        func(json.RawMessage, serverrequest.ToolUserInputPayload)
	SendElicitationURLCard       func(json.RawMessage, serverrequest.ElicitationURLPayload)
	SendElicitationFormCard      func(json.RawMessage, serverrequest.ElicitationFormPayload)
	ReplyCodexError              func(json.RawMessage, int, string)
}

type backendInteractionPresenter struct {
	ports BackendInteractionPresenterPorts
}

func BackendInteractionPresenter(ports BackendInteractionPresenterPorts) backendevents.InteractionPresenter {
	return backendInteractionPresenter{ports: ports}
}

func (p backendInteractionPresenter) InteractionRequested(_ context.Context, event application.BackendEvent) error {
	ports := p.ports
	token := json.RawMessage(event.ResponseToken)
	switch event.Kind {
	case application.EventApprovalRequested:
		cwd := ""
		if ports.FindSubmissionByTurn != nil {
			if _, sub := ports.FindSubmissionByTurn(event.ThreadID, event.TurnID); sub != nil && ports.WorkspaceCwd != nil {
				cwd = ports.WorkspaceCwd(sub.WorkspaceID)
			}
		}
		presentation := approval.PresentationForEvent(event, ports.MergeApprovalPresentation, cwd)
		if ports.SendApprovalCardPresentation != nil {
			ports.SendApprovalCardPresentation(token, presentation)
		}
	case application.EventUserInputRequested:
		if event.UserInput == nil {
			return nil
		}
		payload := *event.UserInput
		if len(payload.Questions) == 1 && len(payload.Questions[0].Options) > 0 && len(payload.Questions[0].Options) <= 3 && !payload.Questions[0].MultiSelect && !payload.Questions[0].IsOther {
			if ports.SendUserInputCard != nil {
				ports.SendUserInputCard(token, payload)
			}
		} else if ports.SendUserInputFormCard != nil {
			ports.SendUserInputFormCard(token, payload)
		}
	case application.EventElicitationURLRequested:
		if event.ElicitationURL != nil && ports.SendElicitationURLCard != nil {
			ports.SendElicitationURLCard(token, *event.ElicitationURL)
		}
	case application.EventElicitationFormRequested:
		if event.ElicitationForm != nil && ports.SendElicitationFormCard != nil {
			ports.SendElicitationFormCard(token, *event.ElicitationForm)
		}
	case application.EventRequestRejected:
		if event.Rejected != nil && ports.ReplyCodexError != nil {
			ports.ReplyCodexError(token, event.Rejected.Code, event.Message)
		}
	}
	return nil
}

func CodexErrorReplyPort(owner *frontendruntime.FrontendOwner) func(json.RawMessage, int, string) {
	return func(requestID json.RawMessage, code int, message string) {
		if owner == nil {
			return
		}
		if client := owner.CodexClient(); client != nil {
			_ = client.ReplyError(requestID, code, message)
		}
	}
}
