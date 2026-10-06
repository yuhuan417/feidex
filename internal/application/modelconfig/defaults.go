package modelconfig

import (
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
	"feidex/internal/textutil"
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
		// Dual write rule: a Bot default persists to the backend-global values
		// and to the frontend profile in one revision, so both tiers stay in
		// sync through this single path.
		for setting, raw := range command.Values {
			value := strings.TrimSpace(raw)
			global := routing.GlobalField(v, setting, command.Backend)
			profile := routing.ProfileField(p, setting, command.Backend)
			if global == nil || profile == nil {
				return fmt.Errorf("unsupported %s default setting %q", command.Backend, setting)
			}
			*global, *profile = value, value
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
	return textutil.FirstNonEmpty(values...)
}
