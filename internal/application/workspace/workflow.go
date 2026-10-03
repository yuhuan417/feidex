package workspace

import (
	"context"
	"encoding/json"
	"errors"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/interaction"
	domainworkspace "feidex/internal/domain/workspace"
	"fmt"
	"strings"
	"time"
)

// Workflow owns the creation request and its durable result. Presentation uses
// the returned outcome after persistence, never advances the request itself.
type Workflow struct {
	Forms    *interactionapp.FormService
	Planning *PlanningService
	Creation *CreationService
	Effects  EffectService
}

type CloneCompletion struct {
	CreationResult
	Payload ClonePayload
	Outcome CreationOutcome
	Error   string
}

type WorktreeCompletion struct {
	CreationResult
	Payload WorktreePayload
	Outcome CreationOutcome
	Error   string
}

type Preparation struct {
	Outcome                       CreationOutcome
	WorkspaceID, TargetDir, Error string
	Takeover                      *interaction.PendingRequest
	NewPayload                    NewPayload
}

func (w Workflow) prepareFailure(pending *interaction.PendingRequest, userID string, payload any, cause error, worktree bool, req SwitchRequest) (Preparation, error) {
	var existing *CloneExistingWorkspaceError
	if errors.As(cause, &existing) {
		out := Preparation{Outcome: CreationCompleted, WorkspaceID: existing.WorkspaceID, TargetDir: existing.TargetDir}
		if err := w.Start(pending.ID, pending.Kind, userID, "", payload); err != nil {
			return out, err
		}
		var effects LifecycleEffects
		if req.Binding != nil {
			req.WorkspaceID = existing.WorkspaceID
			var err error
			effects, err = w.Creation.Lifecycle.Switch(req)
			if err != nil {
				if restoreErr := w.Forms.SaveDraft(pending.ID, payload, "pending", 10*time.Minute, ""); restoreErr != nil {
					return out, fmt.Errorf("%w; restore workspace form: %v", err, restoreErr)
				}
				return out, err
			}
		}
		if err := w.Forms.SaveDraft(pending.ID, payload, "resolved", 0, ""); err != nil {
			return out, err
		}
		w.Effects.Apply(effects)
		return out, nil
	}
	var directory *CloneExistingDirError
	if errors.As(cause, &directory) {
		notice := NewTakeoverNotice(directory.TargetDir)
		if worktree {
			notice = "worktree 目标目录已存在，可直接新建工作区接管。\n\n目录已预填为 `" + directory.TargetDir + "`，并已带上建议的 `workspace_id`。"
		}
		newPayload := NewTakeoverPayloadWithNotice(directory.WorkspaceID, directory.TargetDir, notice)
		takeover, err := w.Open("workspace_new", pending.SessionKey, userID, "", newPayload)
		if err != nil {
			return Preparation{}, err
		}
		out := Preparation{Outcome: CreationTakeover, WorkspaceID: directory.WorkspaceID, TargetDir: directory.TargetDir, Takeover: takeover, NewPayload: newPayload}
		return out, w.Forms.SaveDraft(pending.ID, payload, "resolved", 0, "")
	}
	return Preparation{Outcome: CreationFailed, Error: cause.Error()}, w.Forms.SaveDraft(pending.ID, payload, "pending", 10*time.Minute, "")
}

func (w Workflow) PrepareClone(id, userID string, payload ClonePayload, current *domainworkspace.Workspace, req SwitchRequest) (ClonePayload, *ClonePlan, Preparation, error) {
	pending, err := w.Forms.Authorize(id, "workspace_clone", userID)
	if err != nil {
		return payload, nil, Preparation{}, err
	}
	payload.ErrorMessage = ""
	parent := strings.TrimSpace(payload.SelectedParentDir)
	if parent == "" {
		parent = w.Planning.DefaultWorkspaceCloneParent(current)
	}
	payload.SelectedParentDir = parent
	plan, cause := w.Planning.PrepareWorkspaceClonePayload(payload, parent)
	if cause != nil {
		payload.ErrorMessage = cause.Error()
		out, err := w.prepareFailure(pending, userID, payload, cause, false, req)
		return payload, nil, out, err
	}
	payload = w.Planning.ClonePayloadWithPlan(payload, plan)
	return payload, plan, Preparation{}, nil
}

func (w Workflow) PrepareWorktree(id, userID string, payload WorktreePayload, req SwitchRequest) (WorktreePayload, *WorktreePlan, Preparation, error) {
	pending, err := w.Forms.Authorize(id, "workspace_worktree", userID)
	if err != nil {
		return payload, nil, Preparation{}, err
	}
	payload.ErrorMessage = ""
	plan, cause := w.Planning.PrepareWorkspaceWorktree(payload)
	if cause != nil {
		payload.ErrorMessage = cause.Error()
		out, err := w.prepareFailure(pending, userID, payload, cause, true, req)
		return payload, nil, out, err
	}
	payload.BaseWorkspaceID, payload.BranchName, payload.WorkspaceID = plan.BaseWorkspaceID, plan.BranchName, plan.WorkspaceID
	payload.DirectoryName, payload.TargetDir = plan.DirectoryName, plan.TargetDir
	return payload, plan, Preparation{}, nil
}

type NewCompletion struct {
	CreationResult
	Payload  NewPayload
	Existing bool
}

func (w Workflow) SubmitNew(id, userID string, payload NewPayload, req SwitchRequest) (NewCompletion, error) {
	if _, err := w.Forms.Authorize(id, "workspace_new", userID); err != nil {
		return NewCompletion{}, err
	}
	payload.DraftID, payload.SelectedCWD = strings.TrimSpace(payload.DraftID), strings.TrimSpace(payload.SelectedCWD)
	out := NewCompletion{Payload: payload}
	var cause error
	if payload.DraftID == "" {
		cause = fmt.Errorf("请填写 workspace_id")
	}
	if cause == nil && payload.SelectedCWD == "" {
		cause = fmt.Errorf("请先选择目录")
	}
	if cause != nil {
		if err := w.Forms.SaveDraft(id, payload, "", 0, ""); err != nil {
			return out, err
		}
		return out, cause
	}
	if err := w.Start(id, "workspace_new", userID, "", payload); err != nil {
		return out, err
	}
	if existing := w.Planning.WorkspaceByIDAndCWD(payload.DraftID, payload.SelectedCWD); existing != nil {
		out.Existing = true
		out.WorkspaceID, out.TargetDir = existing.ID, existing.Cwd
		if req.Binding != nil {
			req.WorkspaceID = existing.ID
			out.Effects, cause = w.Creation.Lifecycle.Switch(req)
		}
	} else {
		out.CreationResult, cause = w.Creation.CreateAndSwitch(req, payload.DraftID, firstNonEmpty(payload.DraftName, payload.DraftID), payload.SelectedCWD)
	}
	status := "resolved"
	if cause != nil {
		status = "pending"
	}
	if err := w.Forms.SaveDraft(id, payload, status, 10*time.Minute, ""); err != nil {
		return out, err
	}
	if cause == nil {
		w.Effects.Apply(out.Effects)
	}
	return out, cause
}

func NewPayloadFromText(payload NewPayload, text string) (NewPayload, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return payload, fmt.Errorf("格式错误，需发送: workspace_id [name]")
	}
	payload.DraftID, payload.DraftName = parts[0], parts[0]
	if strings.TrimSpace(payload.SelectedCWD) == "" && len(parts) >= 2 {
		payload.SelectedCWD = parts[1]
		if len(parts) > 2 {
			payload.DraftName = strings.Join(parts[2:], " ")
		}
	} else if len(parts) > 1 {
		payload.DraftName = strings.Join(parts[1:], " ")
	}
	return payload, nil
}

func (w Workflow) Start(id, kind, userID, messageID string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var validation error
	claimed := false
	err = w.Forms.Repository.UpdatePending(id, func(current *interaction.PendingRequest) {
		if current == nil || current.Kind != kind || current.Status != "pending" || (current.ExpiresAt > 0 && current.ExpiresAt <= time.Now().Unix()) {
			validation = fmt.Errorf("请求已过期或正在处理")
			return
		}
		if current.OwnerUserID != "" && current.OwnerUserID != userID {
			validation = fmt.Errorf("你没有权限处理这个工作区请求")
			return
		}
		current.Status, current.PayloadJSON = "processing", string(encoded)
		current.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		if current.FeishuMsgID == "" {
			current.FeishuMsgID = messageID
		}
		claimed = true
	})
	if err != nil {
		return err
	}
	if validation != nil {
		return validation
	}
	if !claimed {
		return fmt.Errorf("请求不存在")
	}
	return nil
}

func (w Workflow) FinishClone(ctx context.Context, id string, req SwitchRequest, payload ClonePayload, plan *ClonePlan, report func(string)) (CloneCompletion, error) {
	result, cause := w.Creation.Clone(ctx, req, payload.RepoURL, plan, report)
	out := CloneCompletion{CreationResult: result, Payload: payload, Outcome: Outcome(cause)}
	out.Payload.ErrorMessage = ""
	if cause != nil && out.Outcome != CreationCancelled {
		out.Error = cause.Error()
		out.Payload.ErrorMessage = out.Error
	}
	if out.Outcome == CreationTakeover {
		out.Payload.DraftID = firstNonEmpty(payload.DraftID, result.WorkspaceID)
	}
	err := w.finish(id, out.Payload, out.Outcome)
	if err == nil && out.Outcome == CreationCompleted {
		w.Effects.Apply(out.Effects)
	}
	return out, err
}

func (w Workflow) FinishWorktree(ctx context.Context, id string, req SwitchRequest, payload WorktreePayload, plan *WorktreePlan) (WorktreeCompletion, error) {
	result, cause := w.Creation.Worktree(ctx, req, plan)
	out := WorktreeCompletion{CreationResult: result, Payload: payload, Outcome: Outcome(cause)}
	out.Payload.ErrorMessage = ""
	if cause != nil && out.Outcome != CreationCancelled {
		out.Error = cause.Error()
		out.Payload.ErrorMessage = out.Error
	}
	err := w.finish(id, out.Payload, out.Outcome)
	if err == nil && out.Outcome == CreationCompleted {
		w.Effects.Apply(out.Effects)
	}
	return out, err
}

func (w Workflow) finish(id string, payload any, outcome CreationOutcome) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var validation error
	err = w.Forms.Repository.UpdatePending(id, func(current *interaction.PendingRequest) {
		if current == nil || (current.Kind != "workspace_clone" && current.Kind != "workspace_worktree") || (current.Status != "processing" && current.Status != "cancelling") {
			validation = fmt.Errorf("工作区请求已结束")
			return
		}
		current.PayloadJSON = string(encoded)
		current.Status = "resolved"
		if outcome == CreationFailed {
			current.Status = "pending"
			current.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		}
	})
	if err != nil {
		return err
	}
	return validation
}

func (w Workflow) Open(kind, sessionKey, userID, messageID string, payload any) (*interaction.PendingRequest, error) {
	if kind != "workspace_new" && kind != "workspace_clone" && kind != "workspace_worktree" {
		return nil, fmt.Errorf("invalid workspace workflow: %s", kind)
	}
	return w.Forms.Open("workspace", interaction.PendingRequest{Kind: kind, SessionKey: strings.TrimSpace(sessionKey), OwnerUserID: userID, FeishuMsgID: messageID}, payload, 10*time.Minute)
}
