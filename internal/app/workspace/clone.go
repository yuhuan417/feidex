package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const CloneProgressKeepLines = 6
const ClonePatchInterval = 1200 * time.Millisecond

// CloneProgressReporter is a callback for clone progress lines.
type CloneProgressReporter func(string)

// GitClone runs git clone with progress reporting.
var GitClone = func(ctx context.Context, repoURL, targetDir string, report CloneProgressReporter) error {
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
		if err := ReadCloneOutput(r, recordLine); err != nil {
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

// CloneTracker tracks clone operations by request ID.
type CloneTracker struct {
	Mu  sync.Mutex
	Ops map[string]*CloneOperation
}

// NewCloneTracker creates a new clone tracker.
func NewCloneTracker() *CloneTracker {
	return &CloneTracker{Ops: map[string]*CloneOperation{}}
}

// CloneOperation tracks a single clone operation's state.
type CloneOperation struct {
	mu             sync.Mutex
	Cancel         context.CancelFunc
	StartedAt      time.Time
	LastProgressAt time.Time
	LastPatchAt    time.Time
	State          string
	Lines          []string
}

// NewCloneOperation creates a new clone operation.
func NewCloneOperation(cancel context.CancelFunc) *CloneOperation {
	now := time.Now()
	return &CloneOperation{
		Cancel:         cancel,
		StartedAt:      now,
		LastProgressAt: now,
		State:          "running",
	}
}

// Snapshot returns a snapshot of the clone operation state.
func (op *CloneOperation) Snapshot() CloneProgressSnapshot {
	if op == nil {
		return CloneProgressSnapshot{}
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.snapshotLocked()
}

func (op *CloneOperation) snapshotLocked() CloneProgressSnapshot {
	snapshot := CloneProgressSnapshot{
		StartedAt:      op.StartedAt,
		LastProgressAt: op.LastProgressAt,
		State:          op.State,
	}
	if len(op.Lines) > 0 {
		snapshot.Lines = append([]string(nil), op.Lines...)
	}
	return snapshot
}

// RecordProgress records a progress line and returns whether a card patch is needed.
func (op *CloneOperation) RecordProgress(line string) (CloneProgressSnapshot, bool) {
	if op == nil {
		return CloneProgressSnapshot{}, false
	}
	line = strings.TrimSpace(line)
	now := time.Now()
	op.mu.Lock()
	defer op.mu.Unlock()
	if line != "" {
		if len(op.Lines) == 0 || op.Lines[len(op.Lines)-1] != line {
			op.Lines = append(op.Lines, line)
			if len(op.Lines) > CloneProgressKeepLines {
				op.Lines = append([]string(nil), op.Lines[len(op.Lines)-CloneProgressKeepLines:]...)
			}
		}
		op.LastProgressAt = now
	}
	shouldPatch := op.LastPatchAt.IsZero() || now.Sub(op.LastPatchAt) >= ClonePatchInterval
	if shouldPatch {
		op.LastPatchAt = now
	}
	return op.snapshotLocked(), shouldPatch
}

// RequestCancel requests cancellation of the clone operation.
func (op *CloneOperation) RequestCancel() CloneProgressSnapshot {
	if op == nil {
		return CloneProgressSnapshot{}
	}
	op.mu.Lock()
	if strings.TrimSpace(op.State) == "" || op.State == "running" {
		op.State = "cancelling"
	}
	op.LastPatchAt = time.Now()
	snapshot := op.snapshotLocked()
	cancel := op.Cancel
	op.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return snapshot
}

// ReadCloneOutput reads lines from a git clone output reader.
func ReadCloneOutput(r io.Reader, emit func(string)) error {
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
