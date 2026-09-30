package claudecli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Live probe: does can_use_tool carry enough information to attribute a
// permission request to the exact subagent that produced it?
//
// Manual only, mirroring the Codex live-test rule:
//
//	FEIDEX_CLAUDE_RUN_TOKEN_TESTS=1 go test ./internal/claudecli/ \
//	  -run TestLiveClaudeSubagentPermissionAttribution -v -timeout 10m
//
// It reads the raw NDJSON stream (not Session) so nothing is filtered out.
func TestLiveClaudeSubagentPermissionAttribution(t *testing.T) {
	if os.Getenv("FEIDEX_CLAUDE_RUN_TOKEN_TESTS") != "1" {
		t.Skip("set FEIDEX_CLAUDE_RUN_TOKEN_TESTS=1 to run the live Claude probe")
	}

	workDir := t.TempDir()
	probeFile := filepath.Join(workDir, "probe-output.txt")

	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", "default",
		"--permission-prompt-tool", "stdio",
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = workDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start claude: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	lines := make(chan []byte)
	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				lines <- line
			}
			if readErr != nil {
				return
			}
		}
	}()

	prompt := fmt.Sprintf(
		"Do this exactly, nothing else: call the Agent tool with run_in_background=true, "+
			"subagent_type=general-purpose and prompt='Use the Write tool to create the file %s containing the text probe'. "+
			"Then end your turn immediately and reply with only: launched",
		probeFile,
	)
	if _, err := fmt.Fprintf(stdin, `{"type":"user","message":{"role":"user","content":%s}}`+"\n", mustJSONString(prompt)); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	t.Log("SENT prompt: launch a background subagent that writes a file")

	type taskInfo struct {
		TaskID, ToolUseID, SubagentType, Status string
		Backgrounded                            *bool
	}
	type permissionInfo struct {
		RequestID, ToolName, ToolUseID, AgentID string
		At                                      time.Time
	}

	var (
		tasks         []taskInfo
		permissions   []permissionInfo
		cancels       []string
		responses     []string
		pendingPerm   *permissionInfo
		sawResult     bool
		interruptSent bool
	)

	overall := time.NewTimer(4 * time.Minute)
	defer overall.Stop()
	idle := time.NewTimer(90 * time.Second)
	defer idle.Stop()
	resetIdleTo := func(d time.Duration) {
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
		idle.Reset(d)
	}
	resetIdle := func() { resetIdleTo(90 * time.Second) }

probe:
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				break probe
			}
			resetIdle()
			var frame map[string]any
			if json.Unmarshal(line, &frame) != nil {
				continue
			}
			switch stringField(frame, "type") {
			case "system":
				subtype := stringField(frame, "subtype")
				if subtype != "task_started" && subtype != "task_updated" && subtype != "task_notification" {
					continue
				}
				info := taskInfo{
					TaskID:       stringField(frame, "task_id"),
					ToolUseID:    stringField(frame, "tool_use_id"),
					SubagentType: stringField(frame, "subagent_type"),
					Status:       stringField(frame, "status"),
				}
				if raw, ok := frame["is_backgrounded"].(bool); ok {
					info.Backgrounded = &raw
				}
				tasks = append(tasks, info)
				t.Logf("FRAME system/%s task_id=%s tool_use_id=%s subagent_type=%s is_backgrounded=%s status=%s",
					subtype, info.TaskID, info.ToolUseID, info.SubagentType, boolText(info.Backgrounded), info.Status)
			case "control_request":
				request, _ := frame["request"].(map[string]any)
				if request == nil {
					continue
				}
				requestID := stringField(frame, "request_id")
				if subtype := stringField(request, "subtype"); subtype != "can_use_tool" {
					t.Logf("FRAME control_request subtype=%s request_id=%s", subtype, requestID)
					continue
				}
				info := permissionInfo{
					RequestID: requestID,
					ToolName:  stringField(request, "tool_name"),
					ToolUseID: stringField(request, "tool_use_id"),
					AgentID:   stringField(request, "agent_id"),
					At:        time.Now(),
				}
				permissions = append(permissions, info)
				pendingPerm = &info
				t.Logf("FRAME control_request can_use_tool request_id=%s tool=%s tool_use_id=%s agent_id=%q keys=%s",
					info.RequestID, info.ToolName, info.ToolUseID, info.AgentID, strings.Join(mapKeys(request), ","))
				// Deliberately not answered: we want to observe whether the
				// main turn can finish while this prompt is parked, and
				// whether the CLI withdraws it when interrupted.
			case "result":
				sawResult = true
				t.Logf("FRAME result subtype=%s is_error=%v", stringField(frame, "subtype"), frame["is_error"])
				if pendingPerm != nil {
					t.Logf("OBSERVATION: main turn result arrived %s after the parked prompt %s (main turn did NOT wait for it)",
						time.Since(pendingPerm.At).Round(time.Millisecond), pendingPerm.RequestID)
					// Ordering captured; finish promptly instead of idling.
					resetIdleTo(5 * time.Second)
				}
				if pendingPerm != nil && !interruptSent {
					interruptSent = true
					if _, err := fmt.Fprintf(stdin, `{"type":"control_request","request_id":"probe-interrupt","request":{"subtype":"interrupt"}}`+"\n"); err != nil {
						t.Logf("interrupt write failed: %v", err)
					} else {
						t.Log("SENT interrupt while the permission prompt is still parked")
					}
				}
			case "control_cancel_request":
				requestID := stringField(frame, "request_id")
				cancels = append(cancels, requestID)
				t.Logf("FRAME control_cancel_request request_id=%s", requestID)
			case "control_response":
				response, _ := frame["response"].(map[string]any)
				responses = append(responses, stringField(response, "request_id")+":"+stringField(response, "subtype"))
			}
		case <-idle.C:
			t.Log("probe idle timeout")
			break probe
		case <-overall.C:
			t.Log("probe overall timeout")
			break probe
		}
	}

	t.Log("=== SUMMARY ===")
	for _, task := range tasks {
		t.Logf("task_started task_id=%s tool_use_id=%s subagent_type=%s backgrounded=%s",
			task.TaskID, task.ToolUseID, task.SubagentType, boolText(task.Backgrounded))
	}
	for _, perm := range permissions {
		t.Logf("can_use_tool request_id=%s tool=%s tool_use_id=%s agent_id=%q", perm.RequestID, perm.ToolName, perm.ToolUseID, perm.AgentID)
		for _, task := range tasks {
			if perm.AgentID != "" && perm.AgentID == task.TaskID {
				t.Logf("  MATCH: agent_id == task_started.task_id (%s)", task.TaskID)
			}
			if perm.ToolUseID != "" && perm.ToolUseID == task.ToolUseID {
				t.Logf("  MATCH: tool_use_id == task_started.tool_use_id (%s)", task.ToolUseID)
			}
		}
	}
	t.Logf("control_cancel_request=%v", cancels)
	t.Logf("control_response=%v", responses)
	t.Logf("main turn finished while prompt parked: %v", sawResult && pendingPerm != nil)

	if len(permissions) == 0 {
		t.Fatal("no can_use_tool request observed; the probe did not reproduce a permission prompt")
	}
	subagentPerms := 0
	for _, perm := range permissions {
		if perm.AgentID != "" {
			subagentPerms++
		}
	}
	if subagentPerms == 0 {
		t.Fatalf("no can_use_tool carried an agent_id: %#v", permissions)
	}
}

func mustJSONString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func boolText(value *bool) string {
	if value == nil {
		return "unset"
	}
	return fmt.Sprintf("%v", *value)
}

// Live probe: when a permission prompt is parked and the turn is interrupted,
// does the CLI withdraw it with control_cancel_request? This is the signal the
// runtime relies on to release the waiting handler and expire the card.
//
//	FEIDEX_CLAUDE_RUN_TOKEN_TESTS=1 go test ./internal/claudecli/ \
//	  -run TestLiveClaudeInterruptWithdrawsParkedPrompt -v -timeout 5m
func TestLiveClaudeInterruptWithdrawsParkedPrompt(t *testing.T) {
	if os.Getenv("FEIDEX_CLAUDE_RUN_TOKEN_TESTS") != "1" {
		t.Skip("set FEIDEX_CLAUDE_RUN_TOKEN_TESTS=1 to run the live Claude probe")
	}

	workDir := t.TempDir()
	probeFile := filepath.Join(workDir, "probe-output.txt")

	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "default",
		"--permission-prompt-tool", "stdio",
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = workDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start claude: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	lines := make(chan []byte)
	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				lines <- line
			}
			if readErr != nil {
				return
			}
		}
	}()

	prompt := fmt.Sprintf(
		"Do this exactly, nothing else: call the Agent tool (do NOT set run_in_background) with "+
			"subagent_type=general-purpose and prompt='Use the Write tool to create the file %s containing the text probe'. "+
			"Wait for the subagent and report its result.",
		probeFile,
	)
	if _, err := fmt.Fprintf(stdin, `{"type":"user","message":{"role":"user","content":%s}}`+"\n", mustJSONString(prompt)); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	t.Log("SENT prompt: launch a foreground subagent that writes a file")

	var (
		requestID     string
		agentID       string
		cancelSeen    bool
		interruptSent bool
	)

	overall := time.NewTimer(3 * time.Minute)
	defer overall.Stop()

probe:
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				break probe
			}
			var frame map[string]any
			if json.Unmarshal(line, &frame) != nil {
				continue
			}
			switch stringField(frame, "type") {
			case "control_request":
				request, _ := frame["request"].(map[string]any)
				if request == nil || stringField(request, "subtype") != "can_use_tool" {
					continue
				}
				requestID = stringField(frame, "request_id")
				agentID = stringField(request, "agent_id")
				t.Logf("FRAME can_use_tool request_id=%s tool=%s agent_id=%q (parking it, not answering)",
					requestID, stringField(request, "tool_name"), agentID)
				if !interruptSent {
					interruptSent = true
					if _, err := fmt.Fprintf(stdin, `{"type":"control_request","request_id":"probe-interrupt","request":{"subtype":"interrupt"}}`+"\n"); err != nil {
						t.Fatalf("interrupt write failed: %v", err)
					}
					t.Log("SENT interrupt while the prompt is parked")
				}
			case "control_cancel_request":
				if stringField(frame, "request_id") == requestID {
					cancelSeen = true
					t.Logf("FRAME control_cancel_request request_id=%s (prompt withdrawn)", requestID)
					break probe
				}
			case "control_response":
				response, _ := frame["response"].(map[string]any)
				t.Logf("FRAME control_response request_id=%s subtype=%s",
					stringField(response, "request_id"), stringField(response, "subtype"))
			case "result":
				t.Logf("FRAME result subtype=%s is_error=%v", stringField(frame, "subtype"), frame["is_error"])
			}
		case <-overall.C:
			t.Log("probe overall timeout")
			break probe
		}
	}

	if requestID == "" {
		t.Fatal("no can_use_tool request observed; probe did not reach a parked prompt")
	}
	if !cancelSeen {
		t.Fatalf("CLI did not withdraw parked prompt %s after interrupt (agent_id=%q)", requestID, agentID)
	}
	t.Logf("VERIFIED: interrupt withdraws a parked permission prompt (agent_id=%q)", agentID)
}
