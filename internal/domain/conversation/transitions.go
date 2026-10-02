package conversation

import (
	"feidex/internal/domain/modelconfig"
	"feidex/internal/textutil"
	"strings"
	"time"
)

const (
	OpKindSubmission = "submission"
	OpKindTurn       = "turn"
)

func ClearThreadContext(sess *Session) {
	if sess == nil {
		return
	}
	sess.AppliedModelConfig = modelconfig.Snapshot{}
	sess.ModelConfigError = ""
	sess.ActiveThreadID = ""
	sess.ActiveThreadWorkspaceID = ""
	sess.ActiveThreadApprovalPolicy = ""
	sess.ActiveThreadSandboxMode = ""
	sess.ActiveThreadMultiAgentMode = ""
	sess.ActiveClaudePermissionMode = ""
	sess.ActiveThreadServiceTier = ""
	sess.ActiveThreadCollaborationMode = nil
	sess.ActiveThreadName = ""
	sess.ActiveThreadPreview = ""
}

func SetThreadContext(sess *Session, workspaceID, threadID, name, preview string) {
	if sess == nil {
		return
	}
	sess.WorkspaceID = strings.TrimSpace(workspaceID)
	sess.ActiveThreadID = strings.TrimSpace(threadID)
	sess.ActiveThreadWorkspaceID = strings.TrimSpace(workspaceID)
	sess.ActiveThreadName = strings.TrimSpace(name)
	sess.ActiveThreadPreview = strings.TrimSpace(preview)
}

func SetThreadDefaults(sess *Session, approvalPolicy, sandboxMode string) {
	if sess == nil {
		return
	}
	sess.ActiveThreadApprovalPolicy = strings.TrimSpace(approvalPolicy)
	sess.ActiveThreadSandboxMode = strings.TrimSpace(sandboxMode)
}

func BackendThreadSnapshot(sess *Session) SessionBackendThread {
	if sess == nil {
		return SessionBackendThread{}
	}
	return SessionBackendThread{
		ThreadID:             strings.TrimSpace(sess.ActiveThreadID),
		WorkspaceID:          strings.TrimSpace(sess.ActiveThreadWorkspaceID),
		ApprovalPolicy:       strings.TrimSpace(sess.ActiveThreadApprovalPolicy),
		SandboxMode:          strings.TrimSpace(sess.ActiveThreadSandboxMode),
		MultiAgentMode:       strings.TrimSpace(sess.ActiveThreadMultiAgentMode),
		ClaudePermissionMode: strings.TrimSpace(sess.ActiveClaudePermissionMode),
		ServiceTier:          strings.TrimSpace(sess.ActiveThreadServiceTier),
		CollaborationMode:    cloneSessionCollaborationMode(sess.ActiveThreadCollaborationMode),
		Name:                 strings.TrimSpace(sess.ActiveThreadName),
		Preview:              strings.TrimSpace(sess.ActiveThreadPreview),
	}
}

func StoreBackendThread(sess *Session, backend string) {
	if sess == nil {
		return
	}
	backend = normalizeBackend(backend)
	if backend == "" {
		return
	}
	if sess.BackendThreads == nil {
		sess.BackendThreads = map[string]SessionBackendThread{}
	}
	snapshot := BackendThreadSnapshot(sess)
	if snapshot == (SessionBackendThread{}) {
		delete(sess.BackendThreads, backend)
		if len(sess.BackendThreads) == 0 {
			sess.BackendThreads = nil
		}
		return
	}
	sess.BackendThreads[backend] = snapshot
}

func ClearBackendThread(sess *Session, backend string) {
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

func RestoreBackendThread(sess *Session, backend string) bool {
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
	sess.ActiveThreadMultiAgentMode = strings.TrimSpace(snapshot.MultiAgentMode)
	sess.ActiveClaudePermissionMode = strings.TrimSpace(snapshot.ClaudePermissionMode)
	sess.ActiveThreadServiceTier = NormalizeServiceTier(snapshot.ServiceTier)
	sess.ActiveThreadCollaborationMode = cloneSessionCollaborationMode(snapshot.CollaborationMode)
	return true
}

func ClearBackendThreads(sess *Session) {
	if sess == nil {
		return
	}
	sess.BackendThreads = nil
}

func EffectiveServiceTier(sess *Session) string {
	if sess != nil {
		return NormalizeServiceTier(sess.ActiveThreadServiceTier)
	}
	return ""
}

// EffectiveApprovalPolicy resolves the thread override before the workspace
// default. The workspace value is passed in by the application layer so the
// domain remains independent of configuration storage types.
func EffectiveApprovalPolicy(sess *Session, workspacePolicy string) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadApprovalPolicy) != "" {
		return strings.TrimSpace(sess.ActiveThreadApprovalPolicy)
	}
	return strings.TrimSpace(workspacePolicy)
}

func EffectiveSandboxMode(sess *Session, workspaceMode string) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadSandboxMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadSandboxMode)
	}
	return strings.TrimSpace(workspaceMode)
}

func EffectiveMultiAgentMode(sess *Session, workspaceMode string) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadMultiAgentMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadMultiAgentMode)
	}
	if strings.TrimSpace(workspaceMode) != "" {
		return strings.TrimSpace(workspaceMode)
	}
	return "explicitRequestOnly"
}

func CanResumeThreadForWorkspace(sess *Session, workspaceID string) bool {
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return false
	}
	activeWorkspace := strings.TrimSpace(sess.ActiveThreadWorkspaceID)
	return activeWorkspace != "" && activeWorkspace == strings.TrimSpace(workspaceID)
}

func SwitchSessionWorkspace(sess *Session, workspaceID string) {
	if sess == nil {
		return
	}
	previousWorkspaceID := strings.TrimSpace(sess.WorkspaceID)
	sess.WorkspaceID = strings.TrimSpace(workspaceID)
	trackRecentWorkspace(sess, sess.WorkspaceID)
	if !HasInFlightSubmission(sess) {
		ClearBackendThreads(sess)
		ClearThreadContext(sess)
		return
	}
	if sess.ActiveThreadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		sess.ActiveThreadWorkspaceID = previousWorkspaceID
	}
}

func trackRecentWorkspace(sess *Session, workspaceID string) {
	ws := strings.TrimSpace(workspaceID)
	if ws == "" {
		return
	}
	filtered := sess.RecentWorkspaceIDs[:0]
	for _, id := range sess.RecentWorkspaceIDs {
		if id != ws {
			filtered = append(filtered, id)
		}
	}
	sess.RecentWorkspaceIDs = append([]string{ws}, filtered...)
}

func cloneSessionCollaborationMode(src *SessionCollaborationMode) *SessionCollaborationMode {
	if src == nil {
		return nil
	}
	cp := *src
	if src.DeveloperInstructions != nil {
		value := *src.DeveloperInstructions
		cp.DeveloperInstructions = &value
	}
	return &cp
}

func EnsureActiveOperations(sess *Session) {
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
	sess.ActiveOperations = append(sess.ActiveOperations, SessionActiveOperation{
		Kind:         kind,
		SubmissionID: strings.TrimSpace(sess.ActiveSubmissionID),
		ThreadID:     strings.TrimSpace(sess.ActiveThreadID),
		TurnID:       strings.TrimSpace(sess.ActiveTurnID),
	})
}

func ResetActiveOperations(sess *Session) {
	if sess == nil {
		return
	}
	sess.ActiveOperations = nil
	SyncLegacyActiveFields(sess)
}

func SyncLegacyActiveFields(sess *Session) {
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

func ForegroundOperation(sess *Session) *SessionActiveOperation {
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

func HasActiveOperations(sess *Session) bool {
	if sess == nil {
		return false
	}
	EnsureActiveOperations(sess)
	return len(sess.ActiveOperations) > 0
}

func HasInFlightSubmission(sess *Session) bool {
	if sess == nil {
		return false
	}
	if HasActiveOperations(sess) {
		return true
	}
	return strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != ""
}

func UpsertActiveOperation(sess *Session, op SessionActiveOperation) {
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

	next := make([]SessionActiveOperation, 0, len(sess.ActiveOperations)+1)
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

func PrependActiveOperation(sess *Session, op SessionActiveOperation) {
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

	next := make([]SessionActiveOperation, 0, len(sess.ActiveOperations)+1)
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

func RemoveActiveOperation(sess *Session, submissionID, turnID string) bool {
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

	next := make([]SessionActiveOperation, 0, len(sess.ActiveOperations))
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

func FindActiveOperationByTurn(sess *Session, turnID string) *SessionActiveOperation {
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

func FindActiveOperationByThread(sess *Session, threadID string) *SessionActiveOperation {
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

func FindPendingSubmissionOperationByThread(sess *Session, threadID string) *SessionActiveOperation {
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

func normalizeBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "codex", "claude":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func NormalizeServiceTier(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "fast") {
		return "fast"
	}
	return ""
}

func activeOperationMatches(op SessionActiveOperation, submissionID, turnID string) bool {
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
	return textutil.FirstNonEmpty(values...)
}

// HasActiveWork includes standalone compaction and the admitted startup window.
func HasActiveWork(sess *Session) bool {
	if sess == nil {
		return false
	}
	if HasActiveOperations(sess) {
		return true
	}
	switch NormalizeSessionStatus(sess.Status) {
	case SessionStatusCompacting, SessionStatusTurnStarting:
		return true
	default:
		return false
	}
}
