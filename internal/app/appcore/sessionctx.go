package appcore

import (
	"feidex/internal/app/sessionctx"
	"feidex/internal/config"
	"feidex/internal/state"
)

// Session state delegation wrappers — these are pure functions that delegate
// to the sessionctx sub-package and are used by multiple app subsystems.

func SessionHasInFlightSubmission(sess *state.Session) bool {
	return sessionctx.HasInFlightSubmission(sess)
}

func ClearSessionThreadContext(sess *state.Session) {
	sessionctx.ClearThreadContext(sess)
}

func SessionBackendThreadSnapshot(sess *state.Session) state.SessionBackendThread {
	return sessionctx.BackendThreadSnapshot(sess)
}

func SessionStoreBackendThread(sess *state.Session, backend string) {
	sessionctx.StoreBackendThread(sess, backend)
}

func SessionClearBackendThread(sess *state.Session, backend string) {
	sessionctx.ClearBackendThread(sess, backend)
}

func SessionRestoreBackendThread(sess *state.Session, backend string) bool {
	return sessionctx.RestoreBackendThread(sess, backend)
}

func ClearSessionBackendThreads(sess *state.Session) {
	sessionctx.ClearBackendThreads(sess)
}

func SetSessionThreadContext(sess *state.Session, workspaceID, threadID, name, preview string) {
	sessionctx.SetThreadContext(sess, workspaceID, threadID, name, preview)
}

func SetSessionThreadDefaults(sess *state.Session, approvalPolicy, sandboxMode string) {
	sessionctx.SetThreadDefaults(sess, approvalPolicy, sandboxMode)
}

func EffectiveThreadApprovalPolicy(sess *state.Session, ws *config.Workspace) string {
	return sessionctx.EffectiveApprovalPolicy(sess, ws)
}

func EffectiveThreadSandboxMode(sess *state.Session, ws *config.Workspace) string {
	return sessionctx.EffectiveSandboxMode(sess, ws)
}

func EffectiveThreadServiceTier(sess *state.Session) string {
	return sessionctx.EffectiveServiceTier(sess)
}

func SessionCanResumeThreadForSubmission(sess *state.Session, sub *state.Submission) bool {
	return sessionctx.CanResumeThreadForSubmission(sess, sub)
}

func SwitchSessionWorkspace(sess *state.Session, workspaceID string) {
	sessionctx.SwitchSessionWorkspace(sess, workspaceID)
}

// SessionResetActiveOperations clears all active operations from a session.
func SessionResetActiveOperations(sess *state.Session) {
	sessionctx.ResetActiveOperations(sess)
}

// SessionHasActiveOperations returns true if the session has any active operations.
func SessionHasActiveOperations(sess *state.Session) bool {
	return sessionctx.HasActiveOperations(sess)
}

