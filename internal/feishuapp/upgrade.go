package feishuapp

import (
	"context"
	appfeatures "feidex/internal/application/features"

	"strings"
	"sync"

	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	"feidex/internal/adapter/feishu/workspacecmd"
	appstate "feidex/internal/adapter/storage/json/scoped"
	appupgrade "feidex/internal/application/upgrade"
	"feidex/internal/config"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
)

type UpgradeInputs struct {
	Context                func() context.Context
	Config                 *config.Config
	ConfigMu               *sync.RWMutex
	ConfiguredBackend      func() string
	FrontendID             string
	FrontendConfigIndex    int
	State                  *appstate.Store
	Feishu                 FeishuClient
	EffectRunner           frontendruntime.EffectRunner
	WorkspacePresentation  *workspacecards.Presentation
	WorkspaceConfiguration *workspacecmd.ConfigService
	Workflow               *appupgrade.Service
}

func BuildUpgrades(inputs UpgradeInputs) appupgradecmd.UpgradeService {
	configView := frontendConfigView{
		cfg: inputs.Config, mu: inputs.ConfigMu,
		frontendID: inputs.FrontendID, frontendConfigIndex: inputs.FrontendConfigIndex,
	}

	deps := appupgradecmd.UpgradeServiceDeps{
		CurrentVersion: func() string { return currentVersion() },
		CurrentGOARCH:  func() string { return currentGOARCH() },
		NormalizeUpgradeVersion: func(raw string) (string, error) {
			return appfeatures.NormalizeUpgradeVersion(raw)
		},
		RenderSystemMenuCard: func(sessionKey string) map[string]any {
			spec, _ := menuGroupSpec("menu.group.system")
			backend := ""
			if inputs.ConfiguredBackend != nil {
				backend = inputs.ConfiguredBackend()
			}
			return renderSystemMenuCardData(backend, planModeTitleForSession(inputs.State, inputs.State != nil, sessionKey, spec.Label), inputs.Feishu, sessionKey)
		},
	}

	adapter := &appupgradecmd.DefaultApp{
		ContextFunc: inputs.Context,
		OutboundFunc: func() appupgradecmd.Outbound {
			return newEffectOutbound(inputs.FrontendID, inputs.EffectRunner)
		},
		CardRendererFunc: func() appupgradecmd.CardRenderer { return simpleStatusCardRenderer{client: inputs.Feishu} },
		StateFunc: func() appupgradecmd.UpgradeState {
			return inputs.State
		},
		CurrentWorkspaceFunc: func(msg *feishu.InboundMessage) (string, *config.Workspace) {
			sessionKey, _, ws := currentWorkspaceForMessage(inputs.WorkspaceConfiguration, msg)
			return sessionKey, ws
		},
		WorkspaceForSessionFunc: func(sessionKey string) *config.Workspace {
			wsID := configView.defaultWorkspaceID()
			if sess := inputs.State.Session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
				wsID = sess.WorkspaceID
			}
			return config.FindWorkspace(inputs.Config, wsID)
		},
		RenderPathPickerCardFunc: func(requestID string, payload appupgradecmd.PathPickerPayload) (map[string]any, error) {
			return inputs.WorkspacePresentation.RenderPathPickerCard(requestID, payload)
		},
		DataDirFunc: func() string {
			return inputs.Config.DataDir
		},
		DaemonNameFunc: func() string {
			inputs.ConfigMu.RLock()
			defer inputs.ConfigMu.RUnlock()
			return strings.TrimSpace(inputs.Config.Daemon.ServiceName)
		},
		MakeSessionKeyFunc: configView.makeSessionKey,
		ReplyInThreadFunc:  func(string) bool { return false },
		MenuCardBodyFunc: func(action, body string) string {
			return menuCardBody(action, body)
		},
	}
	return appupgradecmd.NewUpgradeService(adapter, deps, inputs.Workflow)
}
