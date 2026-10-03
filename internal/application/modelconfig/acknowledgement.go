package modelconfig

import (
	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/modelconfig"
)

type AcknowledgementRepository interface {
	UpdateSession(string, func(*conversation.Session)) (*conversation.Session, error)
}

type AcknowledgementService struct{ Repository AcknowledgementRepository }

// Applied records only settings acknowledged by the backend at a safe boundary.
func (s AcknowledgementService) Applied(sessionKey string, settings domain.Snapshot) error {
	_, err := s.Repository.UpdateSession(sessionKey, func(sess *conversation.Session) {
		if sess != nil {
			sess.AppliedModelConfig = settings
			sess.ModelConfigError = ""
		}
	})
	return err
}
