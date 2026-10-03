package modelconfig

import (
	"errors"
	"fmt"

	"feidex/internal/application/routing"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/routing"
)

type SettingsRepository interface {
	routing.ConfigurationRepository
	Session(string) *conversation.Session
	UpdateSession(string, func(*conversation.Session)) (*conversation.Session, error)
}

type WriteAdmission interface{ ModelConfigBlockedReason() string }

var ErrSaveBlocked = errors.New("model configuration save blocked")

type saveBlockedError struct{ reason string }

func (e saveBlockedError) Error() string { return "模型配置暂不可保存: " + e.reason }
func (e saveBlockedError) Unwrap() error { return ErrSaveBlocked }

type SettingsService struct {
	Repository SettingsRepository
	Admission  WriteAdmission
	Frontend   identity.FrontendID
}

type SettingScope string

const (
	SessionScope SettingScope = "session"
	ProfileScope SettingScope = "profile"
)

type SettingsResult struct {
	Scope SettingScope
	Value string
}

// SaveAuxiliary selects the existing session, otherwise the frontend profile.
// It persists desired overrides without touching active/applied turn settings.
func (s SettingsService) SaveAuxiliary(key, backend string, setting domain.Setting, value string) (SettingsResult, error) {
	if !setting.Auxiliary() {
		return SettingsResult{}, fmt.Errorf("unknown auxiliary model setting %q", setting)
	}
	if s.Admission != nil {
		if reason := s.Admission.ModelConfigBlockedReason(); reason != "" {
			return SettingsResult{}, saveBlockedError{reason: reason}
		}
	}
	value = domain.ClearableValue(value)
	if sess := s.Repository.Session(key); sess != nil {
		_, err := s.Repository.UpdateSession(sess.Key, func(current *conversation.Session) {
			switch setting {
			case domain.PlanModel:
				current.PlanModelOverride = value
			case domain.PlanEffort:
				current.PlanReasoningEffortOverride = value
			case domain.ReviewModel:
				current.ReviewModelOverride = value
			case domain.SubagentModel:
				current.SubagentModelOverride = value
			case domain.SubagentEffort:
				current.SubagentReasoningEffortOverride = value
			case domain.SmallModel:
				current.SmallModelOverride = value
			}
		})
		if err != nil {
			return SettingsResult{}, err
		}
		return SettingsResult{Scope: SessionScope, Value: value}, nil
	}
	_, err := (routing.ConfigurationService{Repository: s.Repository, Frontend: s.Frontend}).SetProfile(backend, setting, value)
	if err != nil {
		return SettingsResult{}, err
	}
	return SettingsResult{Scope: ProfileScope, Value: value}, nil
}
