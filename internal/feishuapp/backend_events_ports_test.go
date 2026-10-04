package feishuapp

import (
	"context"
	"encoding/json"
	"testing"

	"feidex/internal/adapter/feishu/approval"
	"feidex/internal/adapter/feishu/serverrequest"
	"feidex/internal/application"
	domaininteraction "feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
)

func TestBackendInteractionPresenterRoutesApprovalAndUserInput(t *testing.T) {
	var gotApprovalToken json.RawMessage
	var gotApproval approval.Presentation
	var gotQuickToken json.RawMessage
	var gotQuick serverrequest.ToolUserInputPayload
	var gotFormToken json.RawMessage
	var gotForm serverrequest.ToolUserInputPayload
	var gotWorkspace string
	quickCalls, formCalls := 0, 0
	presenter := BackendInteractionPresenter(BackendInteractionPresenterPorts{
		FindSubmissionByTurn: func(threadID, turnID string) (string, *domainsubmission.Submission) {
			if threadID != "thread-1" || turnID != "turn-1" {
				t.Fatalf("lookup identity = %q/%q", threadID, turnID)
			}
			return "session-1", &domainsubmission.Submission{WorkspaceID: "workspace-1"}
		},
		WorkspaceCwd: func(workspaceID string) string {
			if workspaceID != "workspace-1" {
				t.Fatalf("workspace id = %q", workspaceID)
			}
			gotWorkspace = workspaceID
			return "/repo"
		},
		MergeApprovalPresentation: func(p approval.Presentation) approval.Presentation {
			p.Payload.Request["merged"] = true
			return p
		},
		SendApprovalCardPresentation: func(token json.RawMessage, p approval.Presentation) {
			gotApprovalToken, gotApproval = append(json.RawMessage(nil), token...), p
		},
		SendUserInputCard: func(token json.RawMessage, payload serverrequest.ToolUserInputPayload) {
			quickCalls++
			gotQuickToken, gotQuick = append(json.RawMessage(nil), token...), payload
		},
		SendUserInputFormCard: func(token json.RawMessage, payload serverrequest.ToolUserInputPayload) {
			formCalls++
			gotFormToken, gotForm = append(json.RawMessage(nil), token...), payload
		},
	})

	if err := presenter.InteractionRequested(context.Background(), application.BackendEvent{
		Kind: application.EventApprovalRequested, ThreadID: "thread-1", TurnID: "turn-1", ResponseToken: `"approval-1"`,
		Approval: &application.ApprovalRequested{Kind: "file", ItemID: "item-1", Request: map[string]any{"path": "file.txt"}},
	}); err != nil {
		t.Fatal(err)
	}
	if string(gotApprovalToken) != `"approval-1"` || gotWorkspace != "workspace-1" || gotApproval.ItemID != "item-1" || gotApproval.Payload.Request["merged"] != true {
		t.Fatalf("approval routing: token=%s workspace=%q presentation=%+v", gotApprovalToken, gotWorkspace, gotApproval)
	}

	quickPayload := &application.UserInputEvent{ThreadID: "thread-1", TurnID: "turn-1", Questions: []domaininteraction.ToolUserInputQuestion{{
		ID: "choice", Options: []domaininteraction.ToolUserInputOption{{Label: "A"}, {Label: "B"}},
	}}}
	if err := presenter.InteractionRequested(context.Background(), application.BackendEvent{
		Kind: application.EventUserInputRequested, ResponseToken: `"quick-1"`, UserInput: quickPayload,
	}); err != nil {
		t.Fatal(err)
	}
	formPayload := &application.UserInputEvent{ThreadID: "thread-1", TurnID: "turn-1", Questions: []domaininteraction.ToolUserInputQuestion{{ID: "one"}, {ID: "two"}}}
	if err := presenter.InteractionRequested(context.Background(), application.BackendEvent{
		Kind: application.EventUserInputRequested, ResponseToken: `"form-1"`, UserInput: formPayload,
	}); err != nil {
		t.Fatal(err)
	}
	if quickCalls != 1 || formCalls != 1 || string(gotQuickToken) != `"quick-1"` || gotQuick.Questions[0].ID != "choice" || string(gotFormToken) != `"form-1"` || len(gotForm.Questions) != 2 {
		t.Fatalf("user input routing: quick=%d/%+v form=%d/%+v", quickCalls, gotQuick, formCalls, gotForm)
	}
}

func TestBackendInteractionPresenterRoutesElicitationAndRejectedRequests(t *testing.T) {
	var gotURL serverrequest.ElicitationURLPayload
	var gotForm serverrequest.ElicitationFormPayload
	var gotErrorToken json.RawMessage
	var gotErrorCode int
	var gotErrorMessage string
	presenter := BackendInteractionPresenter(BackendInteractionPresenterPorts{
		SendElicitationURLCard:  func(_ json.RawMessage, payload serverrequest.ElicitationURLPayload) { gotURL = payload },
		SendElicitationFormCard: func(_ json.RawMessage, payload serverrequest.ElicitationFormPayload) { gotForm = payload },
		ReplyCodexError: func(token json.RawMessage, code int, message string) {
			gotErrorToken, gotErrorCode, gotErrorMessage = append(json.RawMessage(nil), token...), code, message
		},
	})

	if err := presenter.InteractionRequested(context.Background(), application.BackendEvent{
		Kind: application.EventElicitationURLRequested, ElicitationURL: &application.ElicitationURLEvent{URL: "https://example.test", Message: "open"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := presenter.InteractionRequested(context.Background(), application.BackendEvent{
		Kind: application.EventElicitationFormRequested, ElicitationForm: &application.ElicitationFormEvent{ServerName: "server", Message: "fill"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := presenter.InteractionRequested(context.Background(), application.BackendEvent{
		Kind: application.EventRequestRejected, ResponseToken: `"rejected-1"`, Message: "unsupported", Rejected: &application.RequestRejected{Code: -32601},
	}); err != nil {
		t.Fatal(err)
	}
	if gotURL.URL != "https://example.test" || gotURL.Message != "open" || gotForm.ServerName != "server" || gotForm.Message != "fill" {
		t.Fatalf("elicitation routing: url=%+v form=%+v", gotURL, gotForm)
	}
	if string(gotErrorToken) != `"rejected-1"` || gotErrorCode != -32601 || gotErrorMessage != "unsupported" {
		t.Fatalf("rejected request reply: token=%s code=%d message=%q", gotErrorToken, gotErrorCode, gotErrorMessage)
	}
}
