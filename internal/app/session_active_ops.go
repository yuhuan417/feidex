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

func sessionHasActiveOperations(sess *state.Session) bool {
	return sessionctx.HasActiveOperations(sess)
}

func sessionUpsertActiveOperation(sess *state.Session, op state.SessionActiveOperation) {
	sessionctx.UpsertActiveOperation(sess, op)
}

func sessionRemoveActiveOperation(sess *state.Session, submissionID, turnID string) bool {
	return sessionctx.RemoveActiveOperation(sess, submissionID, turnID)
}
