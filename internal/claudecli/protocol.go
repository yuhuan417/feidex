package claudecli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type ndjsonReader struct {
	scanner *bufio.Scanner
}

func newNDJSONReader(r io.Reader) *ndjsonReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	return &ndjsonReader{scanner: scanner}
}

func (r *ndjsonReader) ReadLine() ([]byte, error) {
	if r.scanner.Scan() {
		line := r.scanner.Bytes()
		out := make([]byte, len(line))
		copy(out, line)
		return out, nil
	}
	if err := r.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

type ndjsonWriter struct {
	w io.Writer
}

func newNDJSONWriter(w io.Writer) *ndjsonWriter {
	return &ndjsonWriter{w: w}
}

func (w *ndjsonWriter) Write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.w.Write(data)
	return err
}

type wireSystemMessage struct {
	Type           string   `json:"type"`
	Subtype        string   `json:"subtype"`
	SessionID      string   `json:"session_id"`
	CWD            string   `json:"cwd,omitempty"`
	Tools          []string `json:"tools,omitempty"`
	Model          string   `json:"model,omitempty"`
	PermissionMode string   `json:"permissionMode,omitempty"`
}

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

type wireModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow"`
}

type wireResultMessage struct {
	Type         string                    `json:"type"`
	IsError      bool                      `json:"is_error"`
	DurationMs   int64                     `json:"duration_ms"`
	Result       string                    `json:"result"`
	TotalCostUSD float64                   `json:"total_cost_usd"`
	Usage        wireUsage                 `json:"usage"`
	ModelUsage   map[string]wireModelUsage `json:"modelUsage"`
}

type wireFlexibleContent struct {
	raw json.RawMessage
}

func (c *wireFlexibleContent) UnmarshalJSON(data []byte) error {
	c.raw = append(c.raw[:0], data...)
	return nil
}

func (c wireFlexibleContent) AsBlocks() ([]wireContentBlock, bool, error) {
	trimmed := bytes.TrimSpace(c.raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	if trimmed[0] != '[' {
		return nil, false, nil
	}
	var rawBlocks []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawBlocks); err != nil {
		return nil, false, err
	}
	blocks := make([]wireContentBlock, 0, len(rawBlocks))
	for _, raw := range rawBlocks {
		block, err := parseWireContentBlock(raw)
		if err != nil {
			return nil, false, err
		}
		blocks = append(blocks, block)
	}
	return blocks, true, nil
}

type wireMessageContent struct {
	ID      string              `json:"id,omitempty"`
	Role    string              `json:"role"`
	Content wireFlexibleContent `json:"content"`
}

type wireAssistantMessage struct {
	Type    string             `json:"type"`
	Message wireMessageContent `json:"message"`
}

type wireUserMessage struct {
	Type    string             `json:"type"`
	Message wireMessageContent `json:"message"`
}

type wireStreamMessage struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

type wireControlRequest struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
}

type wireControlResponse struct {
	Type     string                     `json:"type"`
	Response wireControlResponsePayload `json:"response"`
}

type wireControlResponsePayload struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	Response  any    `json:"response,omitempty"`
	Error     string `json:"error,omitempty"`
}

type wirePermissionAllow struct {
	Behavior           string           `json:"behavior"`
	UpdatedInput       map[string]any   `json:"updatedInput"`
	UpdatedPermissions []map[string]any `json:"updatedPermissions,omitempty"`
}

type wirePermissionDeny struct {
	Behavior  string `json:"behavior"`
	Message   string `json:"message,omitempty"`
	Interrupt bool   `json:"interrupt,omitempty"`
}

type wireUserMessageToSend struct {
	Type    string               `json:"type"`
	Message wireUserMessageInner `json:"message"`
}

type wireUserMessageInner struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type wireControlRequestToSend struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   any    `json:"request"`
}

type wireInterruptRequest struct {
	Subtype string `json:"subtype"`
}

type wireInitializeRequest struct {
	Subtype string         `json:"subtype"`
	Hooks   map[string]any `json:"hooks"`
}

type wireSetPermissionModeRequest struct {
	Subtype string `json:"subtype"`
	Mode    string `json:"mode"`
}

type wireToolUseRequest struct {
	Subtype               string           `json:"subtype"`
	ToolName              string           `json:"tool_name"`
	Input                 map[string]any   `json:"input"`
	PermissionSuggestions []map[string]any `json:"permission_suggestions,omitempty"`
	BlockedPath           *string          `json:"blocked_path,omitempty"`
}

func parseToolUseRequest(raw json.RawMessage) (*wireToolUseRequest, error) {
	var base struct {
		Subtype string `json:"subtype"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}
	if base.Subtype != "can_use_tool" {
		return nil, nil
	}
	var req wireToolUseRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (m wireResultMessage) contextWindow() int {
	best, ok := selectWireResultModelUsage(m.ModelUsage, m.Usage)
	if !ok || best.ContextWindow <= 0 {
		return 0
	}
	return best.ContextWindow
}

func aggregateWireModelUsage(usages map[string]wireModelUsage) (wireModelUsage, bool) {
	if len(usages) == 0 {
		return wireModelUsage{}, false
	}

	var total wireModelUsage
	for _, usage := range usages {
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.CacheReadInputTokens += usage.CacheReadInputTokens
		total.CacheCreationInputTokens += usage.CacheCreationInputTokens
		total.CostUSD += usage.CostUSD
		if usage.ContextWindow > total.ContextWindow {
			total.ContextWindow = usage.ContextWindow
		}
	}
	return total, true
}

func selectWireResultModelUsage(usages map[string]wireModelUsage, usage wireUsage) (wireModelUsage, bool) {
	if len(usages) == 0 {
		return wireModelUsage{}, false
	}

	best := wireModelUsage{}
	bestSet := false
	bestExactMatches := -1
	bestTotalTokens := -1
	bestContextWindow := -1

	for _, candidate := range usages {
		exactMatches := 0
		if candidate.InputTokens == usage.InputTokens {
			exactMatches++
		}
		if candidate.OutputTokens == usage.OutputTokens {
			exactMatches++
		}
		if candidate.CacheReadInputTokens == usage.CacheReadInputTokens {
			exactMatches++
		}
		if candidate.CacheCreationInputTokens == usage.CacheCreationInputTokens {
			exactMatches++
		}
		totalTokens := candidate.InputTokens + candidate.OutputTokens + candidate.CacheReadInputTokens + candidate.CacheCreationInputTokens
		if !bestSet ||
			exactMatches > bestExactMatches ||
			(exactMatches == bestExactMatches && totalTokens > bestTotalTokens) ||
			(exactMatches == bestExactMatches && totalTokens == bestTotalTokens && candidate.ContextWindow > bestContextWindow) {
			best = candidate
			bestSet = true
			bestExactMatches = exactMatches
			bestTotalTokens = totalTokens
			bestContextWindow = candidate.ContextWindow
		}
	}

	return best, bestSet
}

type wireStreamEventType string

const (
	wireMessageStart      wireStreamEventType = "message_start"
	wireContentBlockStart wireStreamEventType = "content_block_start"
	wireContentBlockDelta wireStreamEventType = "content_block_delta"
	wireContentBlockStop  wireStreamEventType = "content_block_stop"
	wireMessageStop       wireStreamEventType = "message_stop"
)

type wireStreamEvent interface {
	eventType() wireStreamEventType
}

type wireMessageStartEvent struct {
	Type wireStreamEventType `json:"type"`
}

func (e wireMessageStartEvent) eventType() wireStreamEventType { return e.Type }

type wireContentBlockStartEvent struct {
	Type         wireStreamEventType `json:"type"`
	Index        int                 `json:"index"`
	ContentBlock json.RawMessage     `json:"content_block"`
}

func (e wireContentBlockStartEvent) eventType() wireStreamEventType { return e.Type }

type wireContentBlockDeltaEvent struct {
	Type  wireStreamEventType `json:"type"`
	Index int                 `json:"index"`
	Delta json.RawMessage     `json:"delta"`
}

func (e wireContentBlockDeltaEvent) eventType() wireStreamEventType { return e.Type }

type wireContentBlockStopEvent struct {
	Type  wireStreamEventType `json:"type"`
	Index int                 `json:"index"`
}

func (e wireContentBlockStopEvent) eventType() wireStreamEventType { return e.Type }

type wireMessageStopEvent struct {
	Type wireStreamEventType `json:"type"`
}

func (e wireMessageStopEvent) eventType() wireStreamEventType { return e.Type }

func parseWireStreamEvent(raw json.RawMessage) (wireStreamEvent, error) {
	var base struct {
		Type wireStreamEventType `json:"type"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}
	switch base.Type {
	case wireMessageStart:
		var event wireMessageStartEvent
		return event, json.Unmarshal(raw, &event)
	case wireContentBlockStart:
		var event wireContentBlockStartEvent
		return event, json.Unmarshal(raw, &event)
	case wireContentBlockDelta:
		var event wireContentBlockDeltaEvent
		return event, json.Unmarshal(raw, &event)
	case wireContentBlockStop:
		var event wireContentBlockStopEvent
		return event, json.Unmarshal(raw, &event)
	case wireMessageStop:
		var event wireMessageStopEvent
		return event, json.Unmarshal(raw, &event)
	default:
		return nil, nil
	}
}

type wireContentBlock interface {
	blockType() string
}

type wireTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (b wireTextBlock) blockType() string { return b.Type }

type wireThinkingBlock struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

func (b wireThinkingBlock) blockType() string { return b.Type }

type wireToolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (b wireToolUseBlock) blockType() string { return b.Type }

func parseWireContentBlock(raw json.RawMessage) (wireContentBlock, error) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}
	switch base.Type {
	case "text":
		var block wireTextBlock
		return block, json.Unmarshal(raw, &block)
	case "thinking":
		var block wireThinkingBlock
		return block, json.Unmarshal(raw, &block)
	case "tool_use":
		var block wireToolUseBlock
		return block, json.Unmarshal(raw, &block)
	default:
		return nil, fmt.Errorf("unknown content block type: %s", base.Type)
	}
}

func parseWireMessage(line []byte) (any, error) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &base); err != nil {
		return nil, err
	}
	switch base.Type {
	case "system":
		var msg wireSystemMessage
		return msg, json.Unmarshal(line, &msg)
	case "assistant":
		var msg wireAssistantMessage
		return msg, json.Unmarshal(line, &msg)
	case "user":
		var msg wireUserMessage
		return msg, json.Unmarshal(line, &msg)
	case "stream_event":
		var msg wireStreamMessage
		return msg, json.Unmarshal(line, &msg)
	case "result":
		var msg wireResultMessage
		return msg, json.Unmarshal(line, &msg)
	case "control_request":
		var msg wireControlRequest
		return msg, json.Unmarshal(line, &msg)
	case "control_response":
		var msg wireControlResponse
		return msg, json.Unmarshal(line, &msg)
	default:
		return nil, fmt.Errorf("unknown message type: %s", base.Type)
	}
}

func ParseQuestionsFromInput(input map[string]any) ([]Question, error) {
	raw, ok := input["questions"]
	if !ok {
		return nil, fmt.Errorf("missing 'questions' field in input")
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("'questions' field is not an array")
	}
	out := make([]Question, 0, len(arr))
	for i, entry := range arr {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("question %d is not an object", i)
		}
		text, ok := m["question"].(string)
		if !ok || text == "" {
			return nil, fmt.Errorf("question %d missing 'question' text", i)
		}
		q := Question{Text: text}
		if optionsRaw, ok := m["options"]; ok {
			options, ok := optionsRaw.([]any)
			if !ok {
				return nil, fmt.Errorf("question %d 'options' is not an array", i)
			}
			q.Options = make([]QuestionOption, 0, len(options))
			for _, option := range options {
				switch value := option.(type) {
				case string:
					q.Options = append(q.Options, QuestionOption{Label: value})
				case map[string]any:
					if label, ok := value["label"].(string); ok {
						q.Options = append(q.Options, QuestionOption{Label: label})
					}
				}
			}
		}
		if multiSelect, ok := m["multiSelect"].(bool); ok {
			q.MultiSelect = multiSelect
		}
		out = append(out, q)
	}
	return out, nil
}

func ParsePlanInfoFromInput(input map[string]any) (PlanInfo, error) {
	info := PlanInfo{}
	if plan, ok := input["plan"].(string); ok {
		info.Plan = plan
	}
	raw, ok := input["allowedPrompts"]
	if !ok {
		return info, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return info, fmt.Errorf("'allowedPrompts' is not an array")
	}
	info.AllowedPrompts = make([]AllowedPrompt, 0, len(arr))
	for _, entry := range arr {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		prompt := AllowedPrompt{}
		if tool, ok := m["tool"].(string); ok {
			prompt.Tool = tool
		}
		if text, ok := m["prompt"].(string); ok {
			prompt.Prompt = text
		}
		info.AllowedPrompts = append(info.AllowedPrompts, prompt)
	}
	return info, nil
}
