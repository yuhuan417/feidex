package modelconfig

import (
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
	"fmt"
	"strings"
)

type DefaultsRevision struct {
	Values  domain.GlobalValues
	Profile *routing.BotProfile
}
type DefaultsRepository interface {
	UpdateDefaults(func(*DefaultsRevision) error) error
}
type DefaultsService struct {
	Repository DefaultsRepository
	Admission  WriteAdmission
	Frontend   string
	Publisher  interface{ PublishDefaults(string) }
}

type DefaultsCommand struct {
	Backend string
	Values  map[routing.Setting]string
	Catalog *domain.ModelListResult
}

func (s DefaultsService) Set(command DefaultsCommand) error {
	if strings.TrimSpace(s.Frontend) == "" {
		s.Frontend = "default"
	}
	if s.Admission != nil {
		if reason := s.Admission.ModelConfigBlockedReason(); reason != "" {
			return saveBlockedError{reason: reason}
		}
	}
	if command.Backend != domain.BackendCodex && command.Backend != domain.BackendClaude {
		return fmt.Errorf("unsupported backend %q", command.Backend)
	}
	err := s.Repository.UpdateDefaults(func(revision *DefaultsRevision) error {
		v := &revision.Values
		if revision.Profile == nil {
			revision.Profile = &routing.BotProfile{ID: "bot-profile-" + routing.ProfileID(s.Frontend), FrontendID: s.Frontend}
		}
		p := revision.Profile
		for setting, raw := range command.Values {
			value := strings.TrimSpace(raw)
			if command.Backend == domain.BackendClaude {
				switch setting {
				case routing.Model:
					v.ClaudeModel, p.ClaudeModel = value, value
				case routing.Effort:
					v.ClaudeEffort, p.ReasoningEffort = value, value
				case routing.SmallModel:
					v.ClaudeSmallModel, p.ClaudeSmallModel = value, value
				case routing.SubagentModel:
					v.ClaudeSubagent, p.ClaudeSubagentModel = value, value
				default:
					return fmt.Errorf("unsupported Claude default setting %q", setting)
				}
			} else {
				switch setting {
				case routing.Model:
					v.Model, p.Model = value, value
				case routing.Effort:
					v.Effort, p.ReasoningEffort = value, value
				case routing.PlanModel:
					v.PlanModel, p.PlanModel = value, value
				case routing.PlanEffort:
					v.PlanEffort, p.PlanReasoningEffort = value, value
				case routing.ReviewModel:
					v.ReviewModel, p.ReviewModel = value, value
				case routing.SubagentModel:
					v.SubagentModel, p.SubagentModel = value, value
				case routing.SubagentEffort:
					v.SubagentEffort, p.SubagentReasoningEffort = value, value
				default:
					return fmt.Errorf("unsupported Codex default setting %q", setting)
				}
			}
		}
		if catalog := command.Catalog; catalog != nil && command.Backend == domain.BackendCodex {
			model := firstDefault(p.Model, v.Model)
			if !ModelSupportsEffort(FindModelEntry(*catalog, model), firstDefault(p.ReasoningEffort, v.Effort)) {
				v.Effort, p.ReasoningEffort = "", ""
			}
			plan := firstDefault(p.PlanModel, v.PlanModel, model)
			if !ModelSupportsEffort(FindModelEntry(*catalog, plan), firstDefault(p.PlanReasoningEffort, v.PlanEffort)) {
				v.PlanEffort, p.PlanReasoningEffort = "", ""
			}
		}
		return nil
	})
	if err == nil && s.Publisher != nil {
		s.Publisher.PublishDefaults(command.Backend)
	}
	return err
}

func firstDefault(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
