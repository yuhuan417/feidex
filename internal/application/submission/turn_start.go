package submission

import (
	"context"
	"feidex/internal/application"
	"feidex/internal/application/backendops"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/submission"
	"fmt"
)

type TurnEffects interface {
	RunStartTurn(context.Context, application.StartTurn) (backendops.TurnResult, error)
}
type CollaborationSettings interface {
	ModeForTurnStart(string, string) *conversation.SessionCollaborationMode
}
type TurnStarter struct {
	Frontend      identity.FrontendID
	Effects       TurnEffects
	Collaboration CollaborationSettings
}

func (s TurnStarter) Start(ctx context.Context, key, threadID string, sub *submission.Submission, cwd, policy, sandbox, tier, model, effort, multi string) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("nil submission")
	}
	request := backendops.StartTurnRequest{ThreadID: threadID, Submission: sub, Cwd: cwd, ApprovalPolicy: policy, SandboxMode: sandbox, ServiceTier: tier, Model: model, Effort: effort, MultiAgentMode: multi}
	if snapshot := sub.ModelConfig; snapshot.Valid {
		if snapshot.CollaborationMode != "" {
			selectedModel, selectedEffort := snapshot.Model, snapshot.Effort
			if snapshot.CollaborationMode == "plan" {
				selectedModel, selectedEffort = snapshot.PlanModel, snapshot.PlanEffort
			}
			if selectedModel != "" {
				request.Collaboration = &conversation.SessionCollaborationMode{Mode: snapshot.CollaborationMode, Model: selectedModel, ReasoningEffort: selectedEffort}
			}
		}
	} else {
		request.Collaboration = s.Collaboration.ModeForTurnStart(key, threadID)
	}
	result, err := s.Effects.RunStartTurn(ctx, application.StartTurn{Frontend: s.Frontend, SessionKey: identity.SessionKey(key), Request: request})
	return result.ID, err
}
