package approval

import (
	"encoding/json"
	"strings"
)

type Kind string

const (
	KindCommand     Kind = "command"
	KindFile        Kind = "file"
	KindPermissions Kind = "permissions"
)

func (k Kind) String() string {
	return string(k)
}

func NormalizeKind(value string) Kind {
	switch strings.TrimSpace(value) {
	case KindCommand.String():
		return KindCommand
	case KindFile.String():
		return KindFile
	case KindPermissions.String():
		return KindPermissions
	default:
		return Kind(strings.TrimSpace(value))
	}
}

type RequestPayload struct {
	Body               string         `json:"body,omitempty"`
	Request            map[string]any `json:"request,omitempty"`
	Permissions        map[string]any `json:"permissions,omitempty"`
	SessionActionLabel string         `json:"session_action_label,omitempty"`
}

type Presentation struct {
	Kind    Kind
	ThreadID string
	TurnID   string
	ItemID   string
	Body     string
	Payload  RequestPayload
}

func ParseStoredPayload(raw string) RequestPayload {
	if strings.TrimSpace(raw) == "" {
		return RequestPayload{}
	}
	var payload RequestPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return RequestPayload{}
	}
	payload.Body = strings.TrimSpace(payload.Body)
	payload.SessionActionLabel = strings.TrimSpace(payload.SessionActionLabel)
	payload.Request = CloneJSONMap(payload.Request)
	payload.Permissions = CloneJSONMap(payload.Permissions)
	return payload
}

func (p RequestPayload) MarshalJSONText() string {
	normalized := RequestPayload{
		Body:               strings.TrimSpace(p.Body),
		Request:            CloneJSONMap(p.Request),
		Permissions:        CloneJSONMap(p.Permissions),
		SessionActionLabel: strings.TrimSpace(p.SessionActionLabel),
	}
	b, _ := json.Marshal(normalized)
	return string(b)
}

func CloneJSONMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		cp := make(map[string]any, len(src))
		for key, value := range src {
			cp[key] = value
		}
		return cp
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		cp := make(map[string]any, len(src))
		for key, value := range src {
			cp[key] = value
		}
		return cp
	}
	return out
}
