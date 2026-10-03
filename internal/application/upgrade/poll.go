package upgrade

import (
	"context"
	"encoding/json"
	"feidex/internal/domain/interaction"
	"strings"
	"time"
)

type Repository interface {
	Pending(string) *interaction.PendingRequest
	PendingRequests() []*interaction.PendingRequest
	UpdatePending(string, func(*interaction.PendingRequest)) error
}
type UnitStatus struct{ ActiveState, Result, JournalTail string }
type Units interface {
	Query(context.Context, string) (*UnitStatus, error)
	Cleanup(context.Context, string) error
}
type Outcome struct {
	RequestID, SessionKey, MessageID, UnitName string
	Success                                    bool
	Error                                      string
}
type Poller struct {
	Repository Repository
	Units      Units
}

func IsRunning(request *interaction.PendingRequest) bool {
	return request != nil && request.Kind == PendingKind && (request.Status == "upgrading" || request.Status == "launching")
}

// Check commits completion before returning external cleanup/presentation work.
func (s Poller) Check(ctx context.Context, pending *interaction.PendingRequest) (*Outcome, error) {
	if !IsRunning(pending) {
		return nil, nil
	}
	var payload Payload
	decodeErr := json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	var status *UnitStatus
	var err error
	if decodeErr == nil && strings.TrimSpace(payload.UnitName) != "" {
		status, err = s.Units.Query(ctx, payload.UnitName)
		if err != nil {
			return nil, err
		}
		if status != nil && (status.ActiveState == "active" || status.ActiveState == "activating") {
			return nil, nil
		}
		if pending.Status == "launching" && status == nil && payload.LaunchStartedAt > 0 && time.Now().Unix()-payload.LaunchStartedAt < 120 {
			return nil, nil
		}
	}
	claimed := false
	if err := s.Repository.UpdatePending(pending.ID, func(current *interaction.PendingRequest) {
		if current.Status == pending.Status && current.PayloadJSON == pending.PayloadJSON {
			current.Status = "resolved"
			claimed = true
		}
	}); err != nil {
		return nil, err
	}
	if !claimed {
		return nil, nil
	}
	out := &Outcome{RequestID: pending.ID, SessionKey: pending.SessionKey, MessageID: pending.FeishuMsgID, UnitName: payload.UnitName}
	if out.SessionKey == "" {
		out.SessionKey = payload.ChatID
	}
	if out.MessageID == "" {
		out.MessageID = payload.FeishuMsgID
	}
	switch {
	case decodeErr != nil:
		out.Error = "升级参数损坏"
	case payload.UnitName == "":
		out.Error = "缺少升级任务标识"
	case status == nil:
		out.Error = "升级任务已不存在，无法确认结果"
	default:
		out.Success = status.Result == "success"
		out.Error = ExtractError(status.JournalTail)
	}
	return out, nil
}

func ExtractError(journal string) string {
	lines := strings.Split(journal, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(line, "error") || strings.Contains(line, "Error") || strings.Contains(line, "failed") || strings.Contains(line, "mismatch") {
			return line
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
