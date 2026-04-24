package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const workspaceCloneProgressKeepLines = 6
const workspaceClonePatchInterval = 1200 * time.Millisecond

type workspaceCloneProgressReporter func(string)

var workspaceGitClone = func(ctx context.Context, repoURL, targetDir string, report workspaceCloneProgressReporter) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--progress", repoURL, targetDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var (
		mu         sync.Mutex
		output     []string
		streamErr  error
		streamWG   sync.WaitGroup
		recordLine = func(line string) {
			line = strings.TrimSpace(line)
			if line == "" {
				return
			}
			mu.Lock()
			output = append(output, line)
			if len(output) > 20 {
				output = append([]string(nil), output[len(output)-20:]...)
			}
			mu.Unlock()
			if report != nil {
				report(line)
			}
		}
	)
	consume := func(r io.Reader) {
		defer streamWG.Done()
		if err := readWorkspaceCloneOutput(r, recordLine); err != nil {
			if ctx.Err() != nil || errors.Is(err, os.ErrClosed) {
				return
			}
			mu.Lock()
			if streamErr == nil {
				streamErr = err
			}
			mu.Unlock()
		}
	}

	streamWG.Add(2)
	go consume(stdout)
	go consume(stderr)
	waitErr := cmd.Wait()
	streamWG.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	mu.Lock()
	message := strings.TrimSpace(strings.Join(output, "\n"))
	err = streamErr
	mu.Unlock()
	if waitErr != nil {
		if message == "" {
			message = waitErr.Error()
		}
		return fmt.Errorf("git clone failed: %s", message)
	}
	if err != nil {
		return err
	}
	return nil
}

type workspaceCloneTracker struct {
	mu  sync.Mutex
	ops map[string]*workspaceCloneOperation
}

func newWorkspaceCloneTracker() *workspaceCloneTracker {
	return &workspaceCloneTracker{ops: map[string]*workspaceCloneOperation{}}
}

func (a *App) workspaceCloneTracker() *workspaceCloneTracker {
	if a == nil {
		return nil
	}
	if a.workspaceCloneOps == nil {
		a.workspaceCloneOps = newWorkspaceCloneTracker()
	}
	return a.workspaceCloneOps
}

type workspaceCloneOperation struct {
	mu             sync.Mutex
	cancel         context.CancelFunc
	startedAt      time.Time
	lastProgressAt time.Time
	lastPatchAt    time.Time
	state          string
	lines          []string
}

func newWorkspaceCloneOperation(cancel context.CancelFunc) *workspaceCloneOperation {
	now := time.Now()
	return &workspaceCloneOperation{
		cancel:         cancel,
		startedAt:      now,
		lastProgressAt: now,
		state:          "running",
	}
}

func (op *workspaceCloneOperation) snapshot() workspaceCloneProgressSnapshot {
	if op == nil {
		return workspaceCloneProgressSnapshot{}
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.snapshotLocked()
}

func (op *workspaceCloneOperation) snapshotLocked() workspaceCloneProgressSnapshot {
	snapshot := workspaceCloneProgressSnapshot{
		StartedAt:      op.startedAt,
		LastProgressAt: op.lastProgressAt,
		State:          op.state,
	}
	if len(op.lines) > 0 {
		snapshot.Lines = append([]string(nil), op.lines...)
	}
	return snapshot
}

func (op *workspaceCloneOperation) recordProgress(line string) (workspaceCloneProgressSnapshot, bool) {
	if op == nil {
		return workspaceCloneProgressSnapshot{}, false
	}
	line = strings.TrimSpace(line)
	now := time.Now()
	op.mu.Lock()
	defer op.mu.Unlock()
	if line != "" {
		if len(op.lines) == 0 || op.lines[len(op.lines)-1] != line {
			op.lines = append(op.lines, line)
			if len(op.lines) > workspaceCloneProgressKeepLines {
				op.lines = append([]string(nil), op.lines[len(op.lines)-workspaceCloneProgressKeepLines:]...)
			}
		}
		op.lastProgressAt = now
	}
	shouldPatch := op.lastPatchAt.IsZero() || now.Sub(op.lastPatchAt) >= workspaceClonePatchInterval
	if shouldPatch {
		op.lastPatchAt = now
	}
	return op.snapshotLocked(), shouldPatch
}

func (op *workspaceCloneOperation) requestCancel() workspaceCloneProgressSnapshot {
	if op == nil {
		return workspaceCloneProgressSnapshot{}
	}
	op.mu.Lock()
	if strings.TrimSpace(op.state) == "" || op.state == "running" {
		op.state = "cancelling"
	}
	op.lastPatchAt = time.Now()
	snapshot := op.snapshotLocked()
	cancel := op.cancel
	op.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return snapshot
}

func readWorkspaceCloneOutput(r io.Reader, emit func(string)) error {
	if r == nil {
		return nil
	}
	reader := bufio.NewReader(r)
	var buf strings.Builder
	flush := func() {
		line := strings.TrimSpace(buf.String())
		buf.Reset()
		if line == "" || emit == nil {
			return
		}
		emit(line)
	}
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				flush()
				return nil
			}
			return err
		}
		switch b {
		case '\r', '\n':
			flush()
		default:
			buf.WriteByte(b)
		}
	}
}

func (a *App) setWorkspaceCloneOperation(requestID string, op *workspaceCloneOperation) {
	if a == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || op == nil {
		return
	}
	tracker := a.workspaceCloneTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.ops == nil {
		tracker.ops = map[string]*workspaceCloneOperation{}
	}
	if previous := tracker.ops[requestID]; previous != nil && previous.cancel != nil && previous != op {
		previous.cancel()
	}
	tracker.ops[requestID] = op
}

func (a *App) workspaceCloneOperation(requestID string) *workspaceCloneOperation {
	if a == nil {
		return nil
	}
	tracker := a.workspaceCloneTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.ops[strings.TrimSpace(requestID)]
}

func (a *App) clearWorkspaceCloneOperation(requestID string) {
	if a == nil {
		return
	}
	tracker := a.workspaceCloneTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.ops, strings.TrimSpace(requestID))
}

func (a *App) patchWorkspaceCloneProgressCard(messageID, requestID string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) {
	if a == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	card := newWorkspaceRenderService(a).renderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
	if err := a.feishu.PatchCard(context.Background(), messageID, card); err != nil {
		slog.Warn("workspace clone progress patch failed",
			"request_id", requestID,
			"message_id", messageID,
			"error", err,
		)
	}
}

func (a *App) noteWorkspaceCloneProgress(op *workspaceCloneOperation, requestID, messageID string, payload workspaceClonePayload, parentDir, line string) {
	if op == nil {
		return
	}
	snapshot, shouldPatch := op.recordProgress(line)
	if shouldPatch {
		a.patchWorkspaceCloneProgressCard(messageID, requestID, payload, parentDir, snapshot)
	}
}

func (a *App) finishWorkspaceCloneSubmit(ctx context.Context, op *workspaceCloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir string, payload workspaceClonePayload) {
	appState := a.appState()
	defer a.clearWorkspaceCloneOperation(requestID)
	slog.Debug("workspace clone started",
		"request_id", requestID,
		"message_id", messageID,
		"session_key", sessionKey,
		"repo_url", payload.RepoURL,
		"parent_dir", parentDir,
	)
	workspaceID, targetDir, err := a.cloneWorkspaceInParent(
		ctx,
		sessionKey,
		userID,
		chatID,
		chatType,
		payload.RepoURL,
		payload.DraftID,
		parentDir,
		func(line string) {
			a.noteWorkspaceCloneProgress(op, requestID, messageID, payload, parentDir, line)
		},
	)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Warn("workspace clone canceled",
				"request_id", requestID,
				"message_id", messageID,
				"session_key", sessionKey,
				"repo_url", payload.RepoURL,
				"parent_dir", parentDir,
			)
			payload.SelectedParentDir = parentDir
			payload.ErrorMessage = ""
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = mustJSON(payload)
				req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				a.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderService(a).renderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, op.snapshot()))
			}
			return
		}
		var takeoverErr *workspaceCloneTakeoverError
		if errors.As(err, &takeoverErr) {
			slog.Warn("workspace clone needs manual takeover",
				"request_id", requestID,
				"message_id", messageID,
				"session_key", sessionKey,
				"repo_url", payload.RepoURL,
				"parent_dir", parentDir,
				"target_dir", takeoverErr.TargetDir,
				"workspace_id", takeoverErr.WorkspaceID,
				"error", takeoverErr.Err,
			)
			payload.SelectedParentDir = parentDir
			payload.DraftID = firstNonEmpty(strings.TrimSpace(payload.DraftID), strings.TrimSpace(takeoverErr.WorkspaceID))
			if takeoverErr.Err != nil {
				payload.ErrorMessage = takeoverErr.Err.Error()
			} else {
				payload.ErrorMessage = err.Error()
			}
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = mustJSON(payload)
				req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				_ = a.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderService(a).renderWorkspaceCloneManualHintCard(sessionKey, payload.DraftID, takeoverErr.TargetDir, payload.ErrorMessage))
			}
			return
		}
		slog.Warn("workspace clone failed",
			"request_id", requestID,
			"message_id", messageID,
			"session_key", sessionKey,
			"repo_url", payload.RepoURL,
			"parent_dir", parentDir,
			"error", err,
		)
		payload.SelectedParentDir = parentDir
		payload.ErrorMessage = err.Error()
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "pending"
			req.PayloadJSON = mustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = a.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderService(a).renderWorkspaceCloneCard(sessionKey, requestID, payload))
		}
		return
	}
	slog.Debug("workspace clone completed",
		"request_id", requestID,
		"message_id", messageID,
		"session_key", sessionKey,
		"workspace_id", workspaceID,
		"target_dir", targetDir,
	)
	payload.SelectedParentDir = parentDir
	payload.ErrorMessage = ""
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = mustJSON(payload)
	})
	if strings.TrimSpace(messageID) != "" {
		_ = a.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderService(a).renderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir))
	}
}

func (a *App) prepareWorkspaceClone(repoURL, explicitID, parentDir string) (*workspaceClonePlan, error) {
	repoName, err := workspaceCloneRepoName(repoURL)
	if err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(explicitID)
	if workspaceID == "" {
		workspaceID = workspaceCloneDefaultID(repoName)
		if workspaceID == "" {
			return nil, fmt.Errorf("无法从 git 地址推导 workspace_id，请手动指定")
		}
	}
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		return nil, fmt.Errorf("请先选择父目录")
	}
	targetName := repoName
	if strings.TrimSpace(explicitID) != "" {
		targetName = workspaceID
	}
	targetDir := filepath.Join(parentDir, targetName)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		if existingWS := a.workspaceByCWD(targetDir); existingWS != nil {
			return nil, &workspaceCloneExistingWorkspaceError{
				WorkspaceID: existingWS.ID,
				TargetDir:   targetDir,
			}
		}
		return nil, &workspaceCloneExistingDirError{
			WorkspaceID: workspaceID,
			TargetDir:   targetDir,
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if config.FindWorkspace(a.cfg, workspaceID) != nil {
		return nil, fmt.Errorf("workspace %q 已存在，请指定新的 workspace_id", workspaceID)
	}
	return &workspaceClonePlan{
		RepoName:    repoName,
		WorkspaceID: workspaceID,
		TargetDir:   targetDir,
	}, nil
}

func (a *App) cloneWorkspaceInParent(ctx context.Context, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir string, report workspaceCloneProgressReporter) (string, string, error) {
	plan, err := a.prepareWorkspaceClone(repoURL, explicitID, parentDir)
	if err != nil {
		return "", "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetDir), 0o755); err != nil {
		return "", "", err
	}
	if err := workspaceGitClone(ctx, strings.TrimSpace(repoURL), plan.TargetDir, report); err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if err := a.createWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, plan.WorkspaceID, plan.WorkspaceID, plan.TargetDir); err != nil {
		return "", "", &workspaceCloneTakeoverError{
			WorkspaceID: plan.WorkspaceID,
			TargetDir:   plan.TargetDir,
			Err:         err,
		}
	}
	return plan.WorkspaceID, plan.TargetDir, nil
}

func (a *App) cloneWorkspaceAndSwitch(msg *feishu.InboundMessage, repoURL, explicitID string) error {
	return a.cloneWorkspaceAndSwitchInSelectedParent(msg, repoURL, explicitID, "")
}

func (a *App) cloneWorkspaceAndSwitchInSelectedParent(msg *feishu.InboundMessage, repoURL, explicitID, parentDir string) error {
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	if strings.TrimSpace(parentDir) == "" {
		parentDir = a.defaultWorkspaceCloneParent(ws)
	}
	workspaceID, targetDir, err := a.cloneWorkspaceInParent(
		context.Background(),
		sessionKey,
		msg.UserID,
		msg.ChatID,
		msg.ChatType,
		repoURL,
		explicitID,
		parentDir,
		nil,
	)
	if err != nil {
		return err
	}
	reply := "已从仓库创建并切换到工作区 " + workspaceID + "\n" + "cwd: " + targetDir
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, a.replyInThreadEnabled(msg.ChatType))
}
