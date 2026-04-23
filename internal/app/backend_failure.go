package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/state"
)

type codexErrorAware interface {
	SetErrorHandler(func(error))
}

func (a *App) configureCodexClientRuntime(client codexClient) {
	if client == nil {
		return
	}
	client.SetHandlers(a.handleNotification, a.handleServerRequest)
	if aware, ok := client.(codexErrorAware); ok {
		aware.SetErrorHandler(func(err error) {
			a.handleCodexTransportError(client, err)
		})
	}
}

func (a *App) handleCodexTransportError(client codexClient, err error) {
	if a == nil || !a.beginCodexTransportRecovery(client) {
		return
	}
	message := "Codex 后端异常退出。"
	if detail := strings.TrimSpace(errorText(err)); detail != "" {
		message = "Codex 后端异常退出：" + detail
	}
	slog.Error("codex backend transport failed",
		"frontend_id", a.frontendID,
		"error", err,
	)
	a.resetLiveThreadState()
	a.runAsync(func() {
		a.failBackendActiveWork(backendCodex, "", "", message)
	})
	a.runAsync(func() {
		a.recoverCodexRuntimeAfterTransportFailure(client)
	})
}

func (a *App) failClaudeSessionActiveWork(sessionKey, threadID string, err error) {
	if runtime := backendRuntimeForKind(backendClaude); runtime != nil {
		runtime.handleTransportFailure(a, sessionKey, threadID, err)
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func (a *App) failBackendActiveWork(backend, scopeSessionKey, scopeThreadID, message string) {
	if a == nil || a.store == nil {
		return
	}
	appState := a.appState()
	sessions := appState.sessions()
	seenSubmissions := map[string]struct{}{}
	type compactTarget struct {
		threadID string
		turnID   string
	}
	compactTargets := make([]compactTarget, 0)
	for _, sess := range sessions {
		if sess == nil || !a.sessionBelongsToFrontend(sess.Key) {
			continue
		}
		if !backendFailureScopeMatches(sess, scopeSessionKey, scopeThreadID) {
			continue
		}
		sessionEnsureActiveOperations(sess)
		if len(sess.ActiveOperations) == 0 && strings.TrimSpace(sess.Status) != sessionStatusCompacting {
			continue
		}
		for _, op := range sess.ActiveOperations {
			if submissionID := strings.TrimSpace(op.SubmissionID); submissionID != "" {
				if _, ok := seenSubmissions[submissionID]; ok {
					continue
				}
				sub := appState.submission(submissionID)
				if sub == nil {
					continue
				}
				seenSubmissions[submissionID] = struct{}{}
				a.failSubmissionWithoutTerminalCompletion(sess.Key, sub, strings.TrimSpace(op.ThreadID), strings.TrimSpace(op.TurnID), message)
				continue
			}
			if normalizeRuntimeBackend(backend) != backendCodex {
				continue
			}
			threadID := firstNonEmpty(strings.TrimSpace(op.ThreadID), strings.TrimSpace(sess.ActiveThreadID))
			if threadID == "" {
				continue
			}
			compactTargets = append(compactTargets, compactTarget{
				threadID: threadID,
				turnID:   strings.TrimSpace(op.TurnID),
			})
		}
		if normalizeRuntimeBackend(backend) == backendCodex && strings.TrimSpace(sess.Status) == sessionStatusCompacting {
			threadID := strings.TrimSpace(sess.ActiveThreadID)
			if threadID != "" {
				compactTargets = append(compactTargets, compactTarget{threadID: threadID})
			}
		}
	}
	seenCompacts := map[string]struct{}{}
	for _, target := range compactTargets {
		key := target.threadID + "|" + target.turnID
		if _, ok := seenCompacts[key]; ok {
			continue
		}
		seenCompacts[key] = struct{}{}
		a.failStandaloneCompactTurn(target.threadID, target.turnID, message)
	}
}

func backendFailureScopeMatches(sess *state.Session, scopeSessionKey, scopeThreadID string) bool {
	if sess == nil {
		return false
	}
	scopeSessionKey = strings.TrimSpace(scopeSessionKey)
	scopeThreadID = strings.TrimSpace(scopeThreadID)
	if scopeSessionKey != "" && strings.TrimSpace(sess.Key) != scopeSessionKey {
		return false
	}
	if scopeThreadID != "" {
		if strings.TrimSpace(sess.ActiveThreadID) == scopeThreadID {
			return true
		}
		for _, op := range sess.ActiveOperations {
			if strings.TrimSpace(op.ThreadID) == scopeThreadID {
				return true
			}
		}
		return false
	}
	return true
}

func (a *App) resolvePendingRequestsForTerminalFailure(sessionKey, threadID, turnID string) {
	if a == nil || a.store == nil {
		return
	}
	appState := a.appState()
	now := time.Now().Unix()
	for _, req := range appState.pendingRequests() {
		if req == nil || !isPendingRequestOpen(req) {
			continue
		}
		if sessionKey != "" && strings.TrimSpace(req.SessionKey) != strings.TrimSpace(sessionKey) {
			continue
		}
		if turnID != "" {
			if strings.TrimSpace(req.TurnID) != strings.TrimSpace(turnID) {
				continue
			}
		} else if threadID != "" && strings.TrimSpace(req.ThreadID) != strings.TrimSpace(threadID) {
			continue
		}
		_ = appState.updatePending(req.ID, func(current *state.PendingRequest) {
			current.Status = "resolved"
			if current.ExpiresAt < now {
				return
			}
			current.ExpiresAt = now
		})
	}
}

func (a *App) failSubmissionWithoutTerminalCompletion(sessionKey string, sub *state.Submission, threadID, turnID, message string) {
	if a == nil || a.store == nil || sub == nil {
		return
	}
	appState := a.appState()
	current := appState.submission(sub.ID)
	if current == nil || current.Finalized {
		return
	}
	sub = current
	threadID = firstNonEmpty(strings.TrimSpace(threadID), strings.TrimSpace(sub.ThreadID))
	turnID = firstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	if turnID != "" && message != "" {
		a.recordTurnError(threadID, turnID, message)
	}
	flush := turnStreamFlushResult{}
	if turnID != "" {
		flush = a.flushTurnStream(context.Background(), threadID, turnID)
	}
	a.resolvePendingRequestsForTerminalFailure(sessionKey, threadID, turnID)
	_ = appState.finalizeSubmission(sub.ID, "failed")
	sub = appState.submission(sub.ID)
	if sub == nil {
		return
	}
	a.clearSubmissionProcessingReactions(sub)
	terminalText := turnCompletionTerminalText(sub.Status, firstNonEmpty(strings.TrimSpace(message), strings.TrimSpace(flush.LastError)))
	if terminalText != "" {
		a.replaceTurnEventCardWithReuse(
			context.Background(),
			sub,
			"任务状态",
			"grey",
			prependAttentionMentionMarkdown(terminalText, a.turnStopAttentionUserID(sub, turnID)),
			"turn_terminal",
			"",
			strings.TrimSpace(flush.WorkingMessageID),
		)
	}
	updatedSess, _ := appState.updateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		sessionRemoveActiveOperation(sess, sub.ID, turnID)
		switch {
		case sessionHasActiveOperations(sess):
			sess.Status = "turn_starting"
			for _, op := range sess.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					sess.Status = "turn_in_progress"
					break
				}
			}
		case len(sess.Queue) > 0 || len(sess.StagedImages) > 0:
			sess.Status = "queued"
		default:
			sess.Status = "idle"
		}
	})
	a.cleanupSubmissionRuntimeState(sub)
	if updatedSess != nil && sessionShouldStartNextSubmissionAsync(updatedSess) {
		a.runAsync(func() {
			newSubmissionWorkflow(a).startNextSubmissionAsync(sessionKey, "backendFailed")
		})
	}
}
