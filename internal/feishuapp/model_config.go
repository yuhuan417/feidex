package feishuapp

import (
	"context"
	"strings"
	"sync"

	"feidex/internal/adapter/feishu/modelconfig"
	appstate "feidex/internal/adapter/storage/json/scoped"
	applicationmodelconfig "feidex/internal/application/modelconfig"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/textutil"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type ModelCommandInputs struct {
	Defaults            *applicationmodelconfig.DefaultsService
	Options             *applicationmodelconfig.OptionsService
	Snapshots           applicationmodelconfig.SnapshotService
	Config              *config.Config
	ConfigMu            *sync.RWMutex
	State               *appstate.Store
	RuntimeOwner        *frontendruntime.FrontendOwner
	FrontendID          string
	FrontendConfigIndex int
	ConfiguredBackend   func() string
}

func BuildModelCommands(inputs ModelCommandInputs) modelconfig.ModelConfigService {
	statusSnapshots := inputs.Snapshots
	statusStore := inputs.State
	statusConfig := inputs.Config
	statusConfigMu := inputs.ConfigMu
	statusBackend := inputs.ConfiguredBackend
	statusFrontendID := inputs.FrontendID
	statusFrontendConfigIndex := inputs.FrontendConfigIndex
	replyRunner := newEffectRunner(inputs.RuntimeOwner)
	replyInThread := false
	sessionConfig := sessionModelConfigSource{
		cfg: inputs.Config, configMu: inputs.ConfigMu, store: inputs.State, snapshots: inputs.Snapshots,
		view:    frontendConfigView{cfg: inputs.Config, mu: inputs.ConfigMu, frontendID: inputs.FrontendID, frontendConfigIndex: inputs.FrontendConfigIndex},
		backend: inputs.ConfiguredBackend,
	}
	return modelconfig.ModelConfigService{
		Defaults:    inputs.Defaults,
		Backend:     inputs.ConfiguredBackend,
		Options:     inputs.Options,
		GetConfig:   func() *config.Config { return inputs.Config },
		GetConfigMu: func() *sync.RWMutex { return inputs.ConfigMu },
		ReplyText: func(ctx context.Context, msgID string, text string, replyInThread bool) error {
			return newEffectOutbound(inputs.FrontendID, replyRunner).ReplyText(ctx, msgID, text, replyInThread)
		},
		ReplyCard: func(ctx context.Context, msgID string, card map[string]any, replyInThread bool) (string, error) {
			return replyCardWithIDEffect(ctx, replyRunner, statusFrontendID, msgID, card, replyInThread)
		},
		RequireCodexClient: func() (modelconfig.CodexClient, error) {
			return runtimeViewOf(inputs.RuntimeOwner).requireCodexGateway()
		},
		MakeSessionKey: func(msg *feishu.InboundMessage) string {
			return SessionKeyBuilder(inputs.FrontendID)(msg)
		},
		NormalizeSessionKey: func(sessionKey string) string {
			return frontendConfigView{frontendID: inputs.FrontendID}.normalizeSessionKey(sessionKey)
		},
		SessionBelongsToFrontend: func(sessionKey string) bool {
			return frontendConfigView{frontendID: inputs.FrontendID}.sessionBelongsToFrontend(sessionKey)
		},
		ReplyInThreadEnabled: func(chatType string) bool {
			return false
		},
		SessionConfig:  sessionConfig.ForSession,
		FormatMenuBody: menuCardBody,
		ModelConfigStatus: func(sessionKey string) string {
			view := frontendConfigView{
				cfg: statusConfig, mu: statusConfigMu, backend: statusBackend(),
				frontendID: statusFrontendID, frontendConfigIndex: statusFrontendConfigIndex,
			}
			return modelConfigStatus(statusSnapshots, statusStore, view, sessionKey)
		},
		ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponseWith(replyRunner, inputs.FrontendID, replyInThread, msg, resp)
		},
	}
}

type sessionModelConfigSource struct {
	cfg       *config.Config
	configMu  *sync.RWMutex
	store     *appstate.Store
	snapshots applicationmodelconfig.SnapshotService
	view      frontendConfigView
	backend   func() string
}

func (s sessionModelConfigSource) ForSession(sessionKey string) *config.Config {
	view := s.view
	if s.backend != nil {
		view.backend = s.backend()
	}
	if s.cfg == nil || !p2pSessionScopeActiveForConfig(view, s.store, sessionKey) {
		return nil
	}
	clone := configReadCopy(s.cfg, s.configMu)
	var sess *conversation.Session
	if s.store != nil {
		sess = s.store.Session(view.normalizeSessionKey(sessionKey))
	}
	values := s.snapshots.Auxiliary(sess)
	main := s.snapshots.Desired(view.configuredBackend(), sess)
	if main.Backend == "claude" {
		clone.Claude.Model, clone.Claude.Effort = main.Model, main.Effort
	} else {
		clone.Codex.Model, clone.Codex.ReasoningEffort = main.Model, main.Effort
	}
	clone.Codex.PlanModel, clone.Codex.PlanReasoningEffort = values.PlanModel, values.PlanEffort
	clone.Codex.ReviewModel, clone.Codex.SubagentModel, clone.Codex.SubagentReasoningEffort = values.ReviewModel, values.SubagentModel, values.SubagentEffort
	clone.Claude.SmallModel, clone.Claude.SubagentModel = values.ClaudeSmallModel, values.ClaudeSubagent
	return clone
}

func p2pSessionScopeActiveForConfig(view frontendConfigView, store *appstate.Store, sessionKey string) bool {
	chatType, chatID := sessionKeyChat(sessionKey)
	if store != nil {
		for _, key := range []string{strings.TrimSpace(sessionKey), view.normalizeSessionKey(sessionKey)} {
			if sess := store.Session(key); sess != nil {
				chatType = textutil.FirstNonEmpty(chatType, strings.TrimSpace(sess.ChatType))
				chatID = textutil.FirstNonEmpty(chatID, strings.TrimSpace(sess.ChatID))
			}
		}
	}
	return strings.TrimSpace(chatID) != "" && strings.EqualFold(strings.TrimSpace(chatType), "p2p")
}
