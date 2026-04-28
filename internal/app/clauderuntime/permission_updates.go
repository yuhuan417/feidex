package clauderuntime

import (
	"encoding/json"
	"strings"

	apputil "feidex/internal/app/apputil"
	appruntime "feidex/internal/app/runtime"
)

type SessionPermissionUpdateType string

const (
	SessionPermissionUpdateTypeSetMode  SessionPermissionUpdateType = "setMode"
	SessionPermissionUpdateTypeAddRules SessionPermissionUpdateType = "addRules"
)

func (t SessionPermissionUpdateType) String() string {
	return string(t)
}

type SessionPermissionUpdate struct {
	Destination string
	Type        SessionPermissionUpdateType
	Mode        string
	Rules       any
	Rule        any
}

func (u SessionPermissionUpdate) Map() map[string]any {
	out := map[string]any{
		"destination": strings.TrimSpace(u.Destination),
		"type":        strings.TrimSpace(u.Type.String()),
	}
	if value := strings.TrimSpace(u.Mode); value != "" {
		out["mode"] = value
	}
	if u.Rules != nil {
		out["rules"] = deepCloneJSONValue(u.Rules)
	}
	if u.Rule != nil {
		out["rule"] = deepCloneJSONValue(u.Rule)
	}
	return out
}

func CopySessionPermissionUpdates(in []SessionPermissionUpdate) []SessionPermissionUpdate {
	if len(in) == 0 {
		return nil
	}
	out := make([]SessionPermissionUpdate, 0, len(in))
	for _, item := range in {
		item.Rules = deepCloneJSONValue(item.Rules)
		item.Rule = deepCloneJSONValue(item.Rule)
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func MapSessionPermissionUpdates(in []SessionPermissionUpdate) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		out = append(out, item.Map())
	}
	return out
}

func NormalizeSessionPermissionUpdate(update map[string]any) (SessionPermissionUpdate, bool) {
	if len(update) == 0 {
		return SessionPermissionUpdate{}, false
	}
	if strings.TrimSpace(apputil.StringValue(update["destination"])) != "session" {
		return SessionPermissionUpdate{}, false
	}
	switch strings.TrimSpace(apputil.StringValue(update["type"])) {
	case SessionPermissionUpdateTypeSetMode.String():
		mode := NormalizePermissionMode(apputil.StringValue(update["mode"]))
		switch mode {
		case string(appruntime.ClaudePermissionModeDefault),
			string(appruntime.ClaudePermissionModeAcceptEdits),
			string(appruntime.ClaudePermissionModeBypass):
		default:
			return SessionPermissionUpdate{}, false
		}
		return SessionPermissionUpdate{
			Destination: "session",
			Type:        SessionPermissionUpdateTypeSetMode,
			Mode:        mode,
		}, true
	case SessionPermissionUpdateTypeAddRules.String():
		if update["rules"] == nil && update["rule"] == nil {
			return SessionPermissionUpdate{}, false
		}
		return SessionPermissionUpdate{
			Destination: "session",
			Type:        SessionPermissionUpdateTypeAddRules,
			Rules:       deepCloneJSONValue(update["rules"]),
			Rule:        deepCloneJSONValue(update["rule"]),
		}, true
	default:
		return SessionPermissionUpdate{}, false
	}
}

func deepCloneJSONValue(value any) any {
	if value == nil {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return value
	}
	return out
}
