package app

import (
	"context"
	"encoding/json"

	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type pendingInputService struct {
	app *App
}
func newPendingInputService(app *App) pendingInputService {
	return pendingInputService{app: app}
}

func sendUserInputFormCard(a *App, requestID json.RawMessage, payload toolUserInputPayload) {
	sessionKey, sub := findSubmissionByTurn(a, payload.ThreadID, payload.TurnID)
	if sub == nil {
		replyCodexError(a, requestID, -32602, "no active session for request_user_input")
		return
	}
	requestKey := requestIDKey(requestID)
	card := renderToolUserInputFormCard(requestKey, payload, toolUserInputFormDrafts{}, sub.UserID)
	err := deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            "tool_request_user_input_form",
		sessionKey:      sessionKey,
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		itemID:          payload.ItemID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_user_input",
		linkKind:        "user_input_card",
	})
	if err == nil {
		return
	}
	replyCodexError(a, requestID, -32603, err.Error())
}

func (s pendingInputService) completeToolUserInputText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	var payload toolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return err
	}
	adapter := serverRequestAdapterForPending(s.app, pending)
	summary, err := adapter.replyTextUserInput(pending, payload, msg.Text)
	if err != nil {
		return err
	}
	_ = newRuntimeStateService(s.app).finalizePendingReply(pending)
	if pending.FeishuMsgID != "" {
		_ = s.app.feishu.PatchCard(context.Background(), pending.FeishuMsgID, s.app.feishu.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}

// Wrappers delegating to pendingforms.

var renderToolUserInputBody = pendingforms.RenderToolUserInputBody

var renderToolUserInputFormCard = pendingforms.RenderToolUserInputFormCard

var renderToolUserInputQuestionElements = pendingforms.RenderToolUserInputQuestionElements

var toolUserInputQuestionMarkdown = pendingforms.ToolUserInputQuestionMarkdown

var toolUserInputQuestionPlaceholder = pendingforms.ToolUserInputQuestionPlaceholder

var buildToolUserInputTextInputElement = pendingforms.BuildToolUserInputTextInputElement

var buildToolUserInputSingleSelectElement = pendingforms.BuildToolUserInputSingleSelectElement

var buildToolUserInputOtherInputElement = pendingforms.BuildToolUserInputOtherInputElement

var buildToolUserInputMultiSelectRows = pendingforms.BuildToolUserInputMultiSelectRows

var toolUserInputOptionText = pendingforms.ToolUserInputOptionText

var toolUserInputInitialOption = pendingforms.ToolUserInputInitialOption

var toolUserInputOtherFieldName = pendingforms.ToolUserInputOtherFieldName

var parseToolUserInputResponse = pendingforms.ParseToolUserInputResponse

var parseQuestionAnswers = pendingforms.ParseQuestionAnswers

var splitAnswerParts = pendingforms.SplitAnswerParts

var summarizeAnswers = pendingforms.SummarizeAnswers

var buildToolUserInputResponseFromSelections = pendingforms.BuildToolUserInputResponseFromSelections

var toolUserInputDraftsFromCardAction = pendingforms.ToolUserInputDraftsFromCardAction

var toolUserInputSelectionsFromDrafts = pendingforms.ToolUserInputSelectionsFromDrafts

var toolUserInputDraftValue = pendingforms.ToolUserInputDraftValue

var toolUserInputMultiDraftValues = pendingforms.ToolUserInputMultiDraftValues

var toolUserInputMultiDraftActionValue = pendingforms.ToolUserInputMultiDraftActionValue

var toolUserInputMultiDraftsFromActionValue = pendingforms.ToolUserInputMultiDraftsFromActionValue

var toolUserInputMultiDraftList = pendingforms.ToolUserInputMultiDraftList

var uniqueToolUserInputParts = pendingforms.UniqueToolUserInputParts

var toggleToolUserInputMultiDraft = pendingforms.ToggleToolUserInputMultiDraft

var actionValueMap = pendingforms.ActionValueMap

var toolUserInputSelectionValue = pendingforms.ToolUserInputSelectionValue

var normalizeToolUserInputSelection = pendingforms.NormalizeToolUserInputSelection
