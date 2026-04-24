package sessionctx

import (
	"strings"
	"time"

	appruntime "feidex/internal/app/runtime"
	"feidex/internal/config"
	"feidex/internal/state"
)

// Thread context lifecycle

func ClearThreadContext(sess *state.Session) {
	if sess == nil {
		return
	}
	sess.ActiveThreadID = ""
	sess.ActiveThreadWorkspaceID = ""
	sess.ActiveThreadApprovalPolicy = ""
	sess.ActiveThreadSandboxMode = ""
	sess.ActiveClaudePermissionMode = ""
	sess.ActiveThreadServiceTier = ""
	sess.ActiveThreadName = ""
	sess.ActiveThreadPreview = ""
}

func SetThreadContext(sess *state.Session, workspaceID, threadID, name, preview string) {
	if sess == nil {
		return
	}
	sess.ActiveThreadID = strings.TrimSpace(threadID)
	sess.ActiveThreadWorkspaceID = strings.TrimSpace(workspaceID)
	sess.ActiveThreadName = strings.TrimSpace(name)
	sess.ActiveThreadPreview = strings.TrimSpace(preview)
}

func SetThreadDefaults(sess *state.Session, approvalPolicy, sandboxMode string) {
	if sess == nil {
		return
	}
	sess.ActiveThreadApprovalPolicy = strings.TrimSpace(approvalPolicy)
	sess.ActiveThreadSandboxMode = strings.TrimSpace(sandboxMode)
}

// Backend thread snapshots

func BackendThreadSnapshot(sess *state.Session) state.SessionBackendThread {
	if sess == nil {
		return state.SessionBackendThread{}
	}
	return state.SessionBackendThread{
		ThreadID:             strings.TrimSpace(sess.ActiveThreadID),
		WorkspaceID:          strings.TrimSpace(sess.ActiveThreadWorkspaceID),
		ApprovalPolicy:       strings.TrimSpace(sess.ActiveThreadApprovalPolicy),
		SandboxMode:          strings.TrimSpace(sess.ActiveThreadSandboxMode),
		ClaudePermissionMode: strings.TrimSpace(sess.ActiveClaudePermissionMode),
		ServiceTier:          strings.TrimSpace(sess.ActiveThreadServiceTier),
		Name:                 strings.TrimSpace(sess.ActiveThreadName),
		Preview:              strings.TrimSpace(sess.ActiveThreadPreview),
	}
}

func StoreBackendThread(sess *state.Session, backend string) {
	if sess == nil {
		return
	}
	backend = normalizeBackend(backend)
	if backend == "" {
		return
	}
	if sess.BackendThreads == nil {
		sess.BackendThreads = map[string]state.SessionBackendThread{}
	}
	snapshot := BackendThreadSnapshot(sess)
	if snapshot == (state.SessionBackendThread{}) {
		delete(sess.BackendThreads, backend)
		if len(sess.BackendThreads) == 0 {
			sess.BackendThreads = nil
		}
		return
	}
	sess.BackendThreads[backend] = snapshot
}

func ClearBackendThread(sess *state.Session, backend string) {
	if sess == nil {
		return
	}
	backend = normalizeBackend(backend)
	if backend == "" || len(sess.BackendThreads) == 0 {
		return
	}
	delete(sess.BackendThreads, backend)
	if len(sess.BackendThreads) == 0 {
		sess.BackendThreads = nil
	}
}

func RestoreBackendThread(sess *state.Session, backend string) bool {
	if sess == nil {
		return false
	}
	backend = normalizeBackend(backend)
	if backend == "" || len(sess.BackendThreads) == 0 {
		ClearThreadContext(sess)
		return false
	}
	snapshot, ok := sess.BackendThreads[backend]
	if !ok {
		ClearThreadContext(sess)
		return false
	}
	if strings.TrimSpace(snapshot.WorkspaceID) != "" {
		sess.WorkspaceID = strings.TrimSpace(snapshot.WorkspaceID)
	}
	SetThreadContext(sess, snapshot.WorkspaceID, snapshot.ThreadID, snapshot.Name, snapshot.Preview)
	sess.ActiveThreadApprovalPolicy = strings.TrimSpace(snapshot.ApprovalPolicy)
	sess.ActiveThreadSandboxMode = strings.TrimSpace(snapshot.SandboxMode)
	sess.ActiveClaudePermissionMode = strings.TrimSpace(snapshot.ClaudePermissionMode)
	sess.ActiveThreadServiceTier = NormalizeServiceTier(snapshot.ServiceTier)
	return true
}

func ClearBackendThreads(sess *state.Session) {
	if sess == nil {
		return
	}
	sess.BackendThreads = nil
}

// Effective value resolution

func EffectiveApprovalPolicy(sess *state.Session, ws *config.Workspace) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadApprovalPolicy) != "" {
		return strings.TrimSpace(sess.ActiveThreadApprovalPolicy)
	}
	if ws != nil {
		return strings.TrimSpace(ws.ApprovalPolicy)
	}
	return ""
}

func EffectiveSandboxMode(sess *state.Session, ws *config.Workspace) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadSandboxMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadSandboxMode)
	}
	if ws != nil {
		return strings.TrimSpace(ws.SandboxMode)
	}
	return ""
}

func EffectiveServiceTier(sess *state.Session) string {
	if sess != nil {
		return NormalizeServiceTier(sess.ActiveThreadServiceTier)
	}
	return ""
}

// Workspace switching

func SwitchSessionWorkspace(sess *state.Session, workspaceID string) {
	if sess == nil {
		return
	}
	previousWorkspaceID := strings.TrimSpace(sess.WorkspaceID)
	sess.WorkspaceID = strings.TrimSpace(workspaceID)
	if !HasInFlightSubmission(sess) {
		ClearBackendThreads(sess)
		ClearThreadContext(sess)
		return
	}
	if sess.ActiveThreadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		sess.ActiveThreadWorkspaceID = previousWorkspaceID
	}
}

func CanResumeThreadForSubmission(sess *state.Session, sub *state.Submission) bool {
	if sess == nil || sub == nil {
		return false
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		return false
	}
	if strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		return false
	}
	return strings.TrimSpace(sess.ActiveThreadWorkspaceID) == strings.TrimSpace(sub.WorkspaceID)
}

// Active operations

const (
	OpKindSubmission = "submission"
	OpKindTurn       = "turn"
)

func EnsureActiveOperations(sess *state.Session) {
	if sess == nil || len(sess.ActiveOperations) > 0 {
		return
	}
	if strings.TrimSpace(sess.ActiveTurnID) == "" && strings.TrimSpace(sess.ActiveSubmissionID) == "" {
		return
	}
	kind := OpKindTurn
	if strings.TrimSpace(sess.ActiveSubmissionID) != "" {
		kind = OpKindSubmission
	}
	sess.ActiveOperations = append(sess.ActiveOperations, state.SessionActiveOperation{
		Kind:         kind,
		SubmissionID: strings.TrimSpace(sess.ActiveSubmissionID),
		ThreadID:     strings.TrimSpace(sess.ActiveThreadID),
		TurnID:       strings.TrimSpace(sess.ActiveTurnID),
	})
}

func ResetActiveOperations(sess *state.Session) {
	if sess == nil {
		return
	}
	sess.ActiveOperations = nil
	SyncLegacyActiveFields(sess)
}

func SyncLegacyActiveFields(sess *state.Session) {
	if sess == nil {
		return
	}
	if len(sess.ActiveOperations) == 0 {
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
		return
	}
	foreground := sess.ActiveOperations[len(sess.ActiveOperations)-1]
	sess.ActiveTurnID = strings.TrimSpace(foreground.TurnID)
	sess.ActiveSubmissionID = strings.TrimSpace(foreground.SubmissionID)
	if strings.TrimSpace(foreground.ThreadID) != "" {
		sess.ActiveThreadID = strings.TrimSpace(foreground.ThreadID)
	}
}

func ForegroundOperation(sess *state.Session) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	EnsureActiveOperations(sess)
	if len(sess.ActiveOperations) == 0 {
		return nil
	}
	op := sess.ActiveOperations[len(sess.ActiveOperations)-1]
	return &op
}

func HasActiveOperations(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	EnsureActiveOperations(sess)
	return len(sess.ActiveOperations) > 0
}

func HasInFlightSubmission(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	if HasActiveOperations(sess) {
		return true
	}
	return strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != ""
}

func UpsertActiveOperation(sess *state.Session, op state.SessionActiveOperation) {
	if sess == nil {
		return
	}
	EnsureActiveOperations(sess)
	op.Kind = strings.TrimSpace(op.Kind)
	op.SubmissionID = strings.TrimSpace(op.SubmissionID)
	op.ThreadID = strings.TrimSpace(op.ThreadID)
	op.TurnID = strings.TrimSpace(op.TurnID)
	if op.Kind == "" {
		if op.SubmissionID != "" {
			op.Kind = OpKindSubmission
		} else {
			op.Kind = OpKindTurn
		}
	}

	next := make([]state.SessionActiveOperation, 0, len(sess.ActiveOperations)+1)
	updated := false
	for i := range sess.ActiveOperations {
		candidate := sess.ActiveOperations[i]
		if activeOperationMatches(candidate, op.SubmissionID, op.TurnID) {
			candidate.Kind = firstNonEmpty(op.Kind, strings.TrimSpace(candidate.Kind))
			candidate.SubmissionID = firstNonEmpty(op.SubmissionID, strings.TrimSpace(candidate.SubmissionID))
			candidate.ThreadID = firstNonEmpty(op.ThreadID, strings.TrimSpace(candidate.ThreadID))
			candidate.TurnID = firstNonEmpty(op.TurnID, strings.TrimSpace(candidate.TurnID))
			if op.StartedAt != 0 {
				candidate.StartedAt = op.StartedAt
			}
			next = append(next, candidate)
			updated = true
			continue
		}
		next = append(next, candidate)
	}
	if !updated {
		if op.StartedAt == 0 {
			op.StartedAt = time.Now().Unix()
		}
		next = append(next, op)
	}
	sess.ActiveOperations = next
	SyncLegacyActiveFields(sess)
}

func PrependActiveOperation(sess *state.Session, op state.SessionActiveOperation) {
	if sess == nil {
		return
	}
	EnsureActiveOperations(sess)
	op.Kind = strings.TrimSpace(op.Kind)
	op.SubmissionID = strings.TrimSpace(op.SubmissionID)
	op.ThreadID = strings.TrimSpace(op.ThreadID)
	op.TurnID = strings.TrimSpace(op.TurnID)
	if op.Kind == "" {
		if op.SubmissionID != "" {
			op.Kind = OpKindSubmission
		} else {
			op.Kind = OpKindTurn
		}
	}

	next := make([]state.SessionActiveOperation, 0, len(sess.ActiveOperations)+1)
	if op.StartedAt == 0 {
		op.StartedAt = time.Now().Unix()
	}
	next = append(next, op)
	for i := range sess.ActiveOperations {
		candidate := sess.ActiveOperations[i]
		if activeOperationMatches(candidate, op.SubmissionID, op.TurnID) {
			next[0].Kind = firstNonEmpty(next[0].Kind, strings.TrimSpace(candidate.Kind))
			next[0].SubmissionID = firstNonEmpty(next[0].SubmissionID, strings.TrimSpace(candidate.SubmissionID))
			next[0].ThreadID = firstNonEmpty(next[0].ThreadID, strings.TrimSpace(candidate.ThreadID))
			next[0].TurnID = firstNonEmpty(next[0].TurnID, strings.TrimSpace(candidate.TurnID))
			if next[0].StartedAt == 0 {
				next[0].StartedAt = candidate.StartedAt
			}
			continue
		}
		next = append(next, candidate)
	}
	sess.ActiveOperations = next
	SyncLegacyActiveFields(sess)
}

func RemoveActiveOperation(sess *state.Session, submissionID, turnID string) bool {
	if sess == nil {
		return false
	}
	EnsureActiveOperations(sess)
	if len(sess.ActiveOperations) == 0 {
		return false
	}
	submissionID = strings.TrimSpace(submissionID)
	turnID = strings.TrimSpace(turnID)
	if submissionID == "" && turnID == "" {
		return false
	}

	next := make([]state.SessionActiveOperation, 0, len(sess.ActiveOperations))
	removed := false
	for _, op := range sess.ActiveOperations {
		if activeOperationMatches(op, submissionID, turnID) {
			removed = true
			continue
		}
		next = append(next, op)
	}
	if !removed {
		return false
	}
	sess.ActiveOperations = next
	SyncLegacyActiveFields(sess)
	return true
}

func FindActiveOperationByTurn(sess *state.Session, turnID string) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	EnsureActiveOperations(sess)
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil
	}
	for i := len(sess.ActiveOperations) - 1; i >= 0; i-- {
		op := sess.ActiveOperations[i]
		if strings.TrimSpace(op.TurnID) == turnID {
			cp := op
			return &cp
		}
	}
	return nil
}

func FindActiveOperationByThread(sess *state.Session, threadID string) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	EnsureActiveOperations(sess)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	for i := len(sess.ActiveOperations) - 1; i >= 0; i-- {
		op := sess.ActiveOperations[i]
		if strings.TrimSpace(op.ThreadID) == threadID {
			cp := op
			return &cp
		}
	}
	return nil
}

func FindPendingSubmissionOperationByThread(sess *state.Session, threadID string) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	EnsureActiveOperations(sess)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	for i := 0; i < len(sess.ActiveOperations); i++ {
		op := sess.ActiveOperations[i]
		if strings.TrimSpace(op.Kind) != OpKindSubmission {
			continue
		}
		if strings.TrimSpace(op.ThreadID) != threadID {
			continue
		}
		if strings.TrimSpace(op.TurnID) != "" {
			continue
		}
		cp := op
		return &cp
	}
	return nil
}

// Helpers

func normalizeBackend(value string) string {
	return appruntime.NormalizeBackend(value)
}

func NormalizeServiceTier(value string) string {
	return appruntime.NormalizeServiceTier(value)
}

func activeOperationMatches(op state.SessionActiveOperation, submissionID, turnID string) bool {
	submissionID = strings.TrimSpace(submissionID)
	turnID = strings.TrimSpace(turnID)
	if submissionID != "" && strings.TrimSpace(op.SubmissionID) == submissionID {
		return true
	}
	if turnID != "" && strings.TrimSpace(op.TurnID) == turnID {
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
