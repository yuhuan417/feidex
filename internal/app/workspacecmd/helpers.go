package workspacecmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func formatTurnElapsedLine(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("elapsed: %ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("elapsed: %dm%ds", minutes, secs)
}

func markdownCodeBlock(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return "```\n" + content + "\n```"
}

func sameWorkspaceCWD(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func defaultWorkspaceCloneRoot(_ *config.Workspace) string {
	return "/"
}

func submenuLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "›"
	}
	return label + " ›"
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	return submenuLabel(commandLabel(label, slash))
}

func defaultWorkspaceCloneParent(ws *config.Workspace, cfgPath string) string {
	if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return filepath.Dir(strings.TrimSpace(ws.Cwd))
	}
	if strings.TrimSpace(cfgPath) != "" {
		return filepath.Dir(strings.TrimSpace(cfgPath))
	}
	return "."
}

func selectedWorkspaceIDForMessage(app appcore.AppConfig, msg *feishu.InboundMessage, sess *state.Session) string {
	return appcore.ResolveWorkspaceSelectionForMessage(app, msg, sess)
}

func selectedWorkspaceIDForSession(app appcore.AppConfig, sess *state.Session) string {
	return appcore.ResolveWorkspaceSelectionForSession(app, sess)
}

func setSelectedWorkspaceForMessage(app appcore.AppConfig, msg *feishu.InboundMessage, workspaceID string) error {
	return appcore.SetWorkspaceSelectionForMessage(app, msg, workspaceID)
}

func setSelectedWorkspaceForSession(app appcore.AppConfig, sess *state.Session, workspaceID string) error {
	return appcore.SetWorkspaceSelectionForSession(app, sess, workspaceID)
}

type workspaceSwitchSessionService interface {
	SaveSession(sess *state.Session) error
	SwitchSessionWorkspace(sess *state.Session, workspaceID string)
	ClearSessionLiveThread(sessionKey string)
}

const (
	workspaceSwitchActiveWorkBlockedText = "当前任务仍在运行，请先等待结束或中断后再切换工作区"
	workspaceSwitchPendingWorkBlockedText = "当前还有待处理消息，请先处理完成后再切换工作区"
)

func workspaceSwitchBlockedReason(sess *state.Session, hasInFlight bool) string {
	if sess == nil {
		return ""
	}
	if hasInFlight {
		return workspaceSwitchActiveWorkBlockedText
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return workspaceSwitchPendingWorkBlockedText
	}
	return ""
}

func sessionCanRetargetWorkspace(sess *state.Session, hasInFlight bool) bool {
	return workspaceSwitchBlockedReason(sess, hasInFlight) == ""
}

func applyWorkspaceSwitch(s workspaceSwitchSessionService, sessionKey string, sess *state.Session, workspaceID string) error {
	if s == nil || sess == nil {
		return nil
	}
	s.ClearSessionLiveThread(strings.TrimSpace(sessionKey))
	s.SwitchSessionWorkspace(sess, workspaceID)
	return s.SaveSession(sess)
}
