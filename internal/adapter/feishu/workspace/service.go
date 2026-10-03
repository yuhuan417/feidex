package workspace

import (
	"fmt"
	"strings"
	"time"

	"feidex/internal/adapter/feishu/menuutil"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/domain/backend"
	domain "feidex/internal/domain/workspace"
	"feidex/internal/feishu"
)

type NewPayload = domain.NewPayload
type ClonePayload = domain.ClonePayload
type WorktreePayload = domain.WorktreePayload
type CloneProgressSnapshot = domain.CloneProgressSnapshot
type WorktreePlan = domain.WorktreePlan
type PathPickerPayload = domain.PathPickerPayload

const CloneModeWorkspace = domain.CloneModeWorkspace
const CloneModeWorktree = domain.CloneModeWorktree

var NormalizeCloneMode = domain.NormalizeCloneMode
var submenuCommandLabel = menuutil.SubmenuCommandLabel

type RenderService struct {
	PathPicker func(string, PathPickerPayload) (map[string]any, error)
}

func (s *RenderService) RenderPathPickerCard(id string, payload PathPickerPayload) (map[string]any, error) {
	if s.PathPicker == nil {
		return nil, fmt.Errorf("path picker renderer is unavailable")
	}
	return s.PathPicker(id, payload)
}

func (s *RenderService) FormatMenuBody(action, body string) string {
	return menuutil.MenuCardBody(action, body)
}

func formatTurnElapsedLine(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("elapsed: %ds", seconds)
	}
	return fmt.Sprintf("elapsed: %dm%ds", seconds/60, seconds%60)
}

func markdownCodeBlock(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return "```\n" + content + "\n```"
}

func appendWorkspaceSummary(lines []string, view workspaceapp.View) []string {
	if ws := view.Current; ws != nil {
		if view.Backend == backend.BackendClaude {
			mode := firstNonEmpty(strings.TrimSpace(ws.ClaudePermissionMode), strings.TrimSpace(view.ClaudePermissionMode), "default")
			override := "跟随全局"
			if value := strings.TrimSpace(ws.ClaudePermissionMode); value != "" {
				override = "`" + value + "`"
			}
			lines = append(lines, "默认 Claude 权限: `"+mode+"`", "工作区覆盖: "+override)
		} else {
			lines = append(lines, "默认 sandbox: `"+ws.SandboxMode+"`", "默认 policy: `"+ws.ApprovalPolicy+"`", "默认 multi-agent: `"+ws.MultiAgentMode+"`")
		}
	}
	if view.Unbound {
		lines = append(lines, "当前 Bot 在本群还没有配置工作区。")
	}
	if preview := view.PendingPreview; preview != "" {
		if view.PendingCount > 0 {
			lines = append(lines, fmt.Sprintf("已暂存原消息 pending queue `%d`，下一条: `%s`", view.PendingCount, preview))
		} else {
			lines = append(lines, "已暂存原消息，配置工作区后会继续处理: `"+preview+"`")
		}
	}
	return lines
}

func workspaceConfigButtons(actions []string, sessionKey string) []feishu.Button {
	type command struct{ label, slash, action string }
	commands := map[string]command{"workspace.sandbox.menu": {"配置默认沙箱", "/workspace sandbox", "workspace.sandbox.menu"}, "workspace.policy.menu": {"配置默认策略", "/workspace policy", "workspace.policy.menu"}, "workspace.multiagent.menu": {"配置多智能体模式", "/workspace multiagent", "workspace.multiagent.menu"}, "workspace.permission_mode.menu": {"默认权限", "/workspace permissions", "workspace.permission_mode.menu"}}
	buttons := make([]feishu.Button, 0, len(actions))
	for _, action := range actions {
		command, ok := commands[action]
		if !ok {
			continue
		}
		buttons = append(buttons, feishu.Button{Text: submenuCommandLabel(command.label, command.slash), Type: "default", Value: map[string]any{"action": command.action, "session_key": sessionKey}})
	}
	return buttons
}
