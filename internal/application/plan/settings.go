package plan

import (
	"context"
	"fmt"
	"strings"
	"time"

	policy "feidex/internal/application/modelconfig"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/modelconfig"
	"feidex/internal/textutil"
)

type SettingsValues struct {
	Experimental                         bool
	Model, Effort, PlanModel, PlanEffort string
}
type SettingsSource interface {
	Values(*conversation.Session) SettingsValues
}
type Catalog interface {
	ListModels(context.Context, int) (modelconfig.ModelListResult, error)
	ListCollaborationModes(context.Context) (modelconfig.CollaborationModeListResponse, error)
}
type SettingsService struct {
	Source  SettingsSource
	Catalog func() (Catalog, error)
	Context func() context.Context
}

func (s SettingsService) CaptureMode(sess *conversation.Session) *conversation.SessionCollaborationMode {
	mode := conversation.NormalizeCollaborationMode(sess.ActiveThreadCollaborationMode)
	if mode != nil && strings.EqualFold(mode.Mode, "default") && mode.ReasoningEffort == "" {
		mode.ReasoningEffort = strings.TrimSpace(s.Source.Values(sess).Effort)
	}
	return mode
}

func (s SettingsService) Resolve(sess *conversation.Session, enabled bool) (*conversation.SessionCollaborationMode, error) {
	values := s.Source.Values(sess)
	model, effort := strings.TrimSpace(values.Model), strings.TrimSpace(values.Effort)
	if !enabled {
		var modes []*conversation.SessionCollaborationMode
		if sess != nil {
			modes = []*conversation.SessionCollaborationMode{sess.ActiveThreadCollaborationMode, sess.BackendThreads["codex"].CollaborationMode}
		}
		for _, mode := range modes {
			if mode != nil && strings.EqualFold(mode.Mode, "default") {
				model = textutil.FirstNonEmpty(model, mode.Model)
				effort = textutil.FirstNonEmpty(effort, mode.ReasoningEffort)
			}
		}
		if model == "" {
			for _, mode := range modes {
				if mode != nil && (!strings.EqualFold(mode.Mode, "plan") || values.PlanModel == "") {
					model = textutil.FirstNonEmpty(model, mode.Model)
				}
			}
		}
		if model != "" {
			return &conversation.SessionCollaborationMode{Mode: "default", Model: model, ReasoningEffort: effort}, nil
		}
	} else if !values.Experimental {
		return nil, fmt.Errorf("当前 Codex runtime 未启用 experimental API，`/plan` 不可用")
	}
	client, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	mode := &conversation.SessionCollaborationMode{Mode: "default"}
	if enabled {
		modes, err := client.ListCollaborationModes(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取 collaboration mode 列表失败: %w", err)
		}
		var preset *modelconfig.CollaborationModeMask
		for i := range modes.Data {
			if modes.Data[i].Mode != nil && strings.TrimSpace(*modes.Data[i].Mode) == "plan" {
				preset = &modes.Data[i]
				break
			}
		}
		if preset == nil {
			return nil, fmt.Errorf("当前 Codex app-server 未提供 `plan` collaboration mode")
		}
		mode.Mode = "plan"
		model = textutil.FirstNonEmpty(values.PlanModel, model)
		effort = strings.TrimSpace(values.PlanEffort)
		if preset.ReasoningEffort != nil {
			mode.PresetReasoningEffort = strings.TrimSpace(*preset.ReasoningEffort)
			effort = textutil.FirstNonEmpty(effort, mode.PresetReasoningEffort)
		}
	}
	if model == "" {
		catalog, err := client.ListModels(ctx, 20)
		if err != nil {
			return nil, fmt.Errorf("读取 model 列表失败: %w", err)
		}
		entry := policy.FindModelEntry(catalog, model)
		if entry == nil {
			return nil, fmt.Errorf("当前 Codex model 不可用，无法配置 collaboration mode")
		}
		model = textutil.FirstNonEmpty(entry.ID, entry.Model)
		if !enabled {
			if effort == "" || !policy.ModelSupportsEffort(entry, effort) {
				effort = strings.TrimSpace(entry.DefaultReasoningEffort)
			}
		}
	}
	mode.Model, mode.ReasoningEffort = model, effort
	return mode, nil
}
