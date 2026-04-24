package app

import (
	"feidex/internal/app/sessionctx"
	"feidex/internal/state"
)

const (
	sessionOpKindSubmission = sessionctx.OpKindSubmission
	sessionOpKindTurn       = sessionctx.OpKindTurn
)

func sessionEnsureActiveOperations(sess *state.Session) {
	sessionctx.EnsureActiveOperations(sess)
}

func sessionResetActiveOperations(sess *state.Session) {
	sessionctx.ResetActiveOperations(sess)
}

func sessionSyncLegacyActiveFields(sess *state.Session) {
	sessionctx.SyncLegacyActiveFields(sess)
}

func sessionForegroundOperation(sess *state.Session) *state.SessionActiveOperation {
	return sessionctx.ForegroundOperation(sess)
}

func sessionHasActiveOperations(sess *state.Session) bool {
	return sessionctx.HasActiveOperations(sess)
}

func sessionUpsertActiveOperation(sess *state.Session, op state.SessionActiveOperation) {
	sessionctx.UpsertActiveOperation(sess, op)
}

func sessionPrependActiveOperation(sess *state.Session, op state.SessionActiveOperation) {
	sessionctx.PrependActiveOperation(sess, op)
}

func sessionRemoveActiveOperation(sess *state.Session, submissionID, turnID string) bool {
	return sessionctx.RemoveActiveOperation(sess, submissionID, turnID)
}

func sessionFindActiveOperationByTurn(sess *state.Session, turnID string) *state.SessionActiveOperation {
	return sessionctx.FindActiveOperationByTurn(sess, turnID)
}

func sessionFindActiveOperationByThread(sess *state.Session, threadID string) *state.SessionActiveOperation {
	return sessionctx.FindActiveOperationByThread(sess, threadID)
}

func sessionFindPendingSubmissionOperationByThread(sess *state.Session, threadID string) *state.SessionActiveOperation {
	return sessionctx.FindPendingSubmissionOperationByThread(sess, threadID)
}

