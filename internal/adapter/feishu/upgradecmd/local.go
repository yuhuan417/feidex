package upgradecmd

import (
	"strings"

	"feidex/internal/config"
	domainworkspace "feidex/internal/domain/workspace"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type PathPickerPayload = domainworkspace.PathPickerPayload

func (s UpgradeService) CommandUpgradeLocalPick(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	sessionKey, ws := s.app.UpgradeCurrentWorkspace(msg)
	requestID, payload, err := s.CreateUpgradeLocalPickerRequest(sessionKey, ws, msg.UserID, "")
	if err != nil {
		return err
	}
	card, err := s.app.UpgradeRenderPathPickerCard(requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := s.app.UpgradeOutbound().ReplyCard(s.app.Context(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return s.useCase.AttachDelivery(requestID, msgID)
}

func (s UpgradeService) CommandUpgradeLocalPath(msg *feishu.InboundMessage, rawPath string) error {
	if msg == nil {
		return nil
	}
	sessionKey, ws := s.app.UpgradeCurrentWorkspace(msg)
	selectedPath, err := s.useCase.Artifacts.Resolve(ws, rawPath)
	if err != nil {
		return err
	}
	requestID, payload, err := s.CreateLocalUpgradeRequest(sessionKey, msg.UserID, "", selectedPath)
	if err != nil {
		return err
	}
	card := s.RenderUpgradeConfirmCard("升级确认", sessionKey, requestID, payload, s.UpgradeLocalConfirmLines(payload.BinaryPath))
	msgID, err := s.app.UpgradeOutbound().ReplyCard(s.app.Context(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return s.useCase.AttachDelivery(requestID, msgID)
}

func (s UpgradeService) CreateUpgradeLocalPickerRequest(sessionKey string, ws *config.Workspace, ownerUserID, feishuMsgID string) (string, PathPickerPayload, error) {
	req, payload, err := s.useCase.OpenPicker(s.context(), sessionKey, ws, ownerUserID, feishuMsgID)
	if err != nil {
		return "", payload, err
	}
	return req.ID, payload, nil
}
func (s UpgradeService) CreateLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath string) (string, UpgradePendingPayload, error) {
	req, payload, err := s.useCase.PrepareLocal(s.context(), sessionKey, ownerUserID, feishuMsgID, selectedPath)
	if err != nil {
		return "", payload, err
	}
	return req.ID, payload, nil
}
func (s UpgradeService) CompleteUpgradeLocalPick(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	ws := s.app.UpgradeWorkspaceForSession(sessionKey)
	requestID, payload, err := s.CreateUpgradeLocalPickerRequest(sessionKey, ws, action.UserID, action.MessageID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	card, err := s.app.UpgradeRenderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择本地 Binary"},
		Card:  rawCard(card),
	}, nil
}

func (s UpgradeService) CompleteUpgradeLocalBinaryConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload PathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	req, upgradePayload, err := s.useCase.SelectLocal(s.context(), pending.ID, action.UserID, action.MessageID, selectedPath)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已选择本地 Binary"},
		Card:  rawCard(s.RenderUpgradeConfirmCard("升级确认", req.SessionKey, req.ID, upgradePayload, s.UpgradeLocalConfirmLines(upgradePayload.BinaryPath))),
	}, nil
}

func (s UpgradeService) UpgradeLocalConfirmLines(binaryPath string) []string {
	return []string{
		"当前版本: `" + s.deps.CurrentVersion() + "`",
		"目标架构: `" + s.deps.CurrentGOARCH() + "`",
		"二进制: `" + binaryPath + "`",
	}
}

func actionSessionKey(action *feishu.CardAction) string {
	return actionStringValue(action, "session_key")
}

func actionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(value)
}
