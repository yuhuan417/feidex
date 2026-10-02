package clauderuntime

import (
	"context"
	"feidex/internal/app/apputil"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	domainmodelconfig "feidex/internal/domain/modelconfig"
)

type modelSettingsClient interface {
	SetModel(context.Context, string) error
	SetEffort(context.Context, string) error
}

// Acknowledgments precede publishing the applied snapshot. On a partial
// failure the old snapshot is retained and the complete change is retried.
func applyModelSettings(ctx context.Context, client modelSettingsClient, applied, desired domainmodelconfig.Snapshot) error {
	if client == nil {
		return fmt.Errorf("Claude session unavailable")
	}
	if applied.Model != desired.Model {
		if err := client.SetModel(ctx, desired.Model); err != nil {
			return err
		}
	}
	if applied.Effort != desired.Effort {
		if err := client.SetEffort(ctx, desired.Effort); err != nil {
			return err
		}
	}
	return nil
}

func modelSettingsFromConfig(cfg config.ClaudeConfig, model string) domainmodelconfig.Snapshot {
	return domainmodelconfig.Snapshot{Valid: true, Backend: "claude", Model: strings.TrimSpace(apputil.FirstNonEmpty(model, cfg.Model)),
		Effort: strings.TrimSpace(cfg.Effort), SmallModel: strings.TrimSpace(cfg.SmallModel), SubagentModel: strings.TrimSpace(cfg.SubagentModel)}
}

func (s *Service) desiredModelSettings(sessionKey string, cfg config.ClaudeConfig, model string) domainmodelconfig.Snapshot {
	if s.deps.ModelSettings != nil {
		settings := s.deps.ModelSettings(sessionKey)
		settings.Model = apputil.FirstNonEmpty(settings.Model, model, cfg.Model)
		return settings
	}
	settings := modelSettingsFromConfig(cfg, model)
	if s.deps.AuxiliaryModels != nil {
		settings.SmallModel, settings.SubagentModel = s.deps.AuxiliaryModels(sessionKey)
	}
	return settings
}

func (s *Service) noteModelSettingsApplied(current *SessionState, desired domainmodelconfig.Snapshot) {
	current.Mu.Lock()
	current.AppliedModelConfig = desired
	current.Model = desired.Model
	current.AuxiliarySmallModel, current.AuxiliarySubagentModel = desired.SmallModel, desired.SubagentModel
	current.Mu.Unlock()
	s.configPending.Delete(current.SessionKey)
	if s.deps.ModelSettingsApplied != nil {
		s.deps.ModelSettingsApplied(current.SessionKey, desired)
	}
}

func (s *Service) modelChangeBlockedReason(current *SessionState) string {
	if s.deps.Lookup.GetSession != nil && s.deps.Lookup.SessionHasActiveOps != nil {
		if sess := s.deps.Lookup.GetSession(current.SessionKey); sess != nil {
			status := conversation.NormalizeSessionStatus(sess.Status)
			if s.deps.Lookup.SessionHasActiveOps(sess) || status == conversation.SessionStatusCompacting || status == conversation.SessionStatusTurnStarting {
				return "当前会话仍有运行中的任务；配置待下一轮生效"
			}
		}
	}
	s.mu.Lock()
	for _, pending := range s.pending {
		if pending != nil && pending.Session == current {
			s.mu.Unlock()
			return "当前会话仍有待处理审批或表单"
		}
	}
	s.mu.Unlock()
	current.Mu.Lock()
	defer current.Mu.Unlock()
	if len(current.Turns) > 0 || current.CurrentTurnNumber != 0 || current.InterruptPending {
		return "当前轮尚未结束"
	}
	if current.PendingBackgroundAgentCount > 0 || current.PendingWorkflowCount > 0 {
		return "当前会话仍有后台任务"
	}
	for _, task := range current.BackgroundTasks {
		if task != nil && task.Live && !task.Notified {
			return "当前会话仍有后台任务"
		}
	}
	return ""
}
