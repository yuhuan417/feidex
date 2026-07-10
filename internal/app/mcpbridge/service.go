package mcpbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/sessionctx"
	"feidex/internal/app/turnitem"
	"feidex/internal/state"
)

const (
	ServerID       = "feidex-send"
	BearerEnvName  = "FEIDEX_MCP_TOKEN"
	Protocol       = "2025-03-26"
	Path           = "/mcp"
	SessionKeyName = "session_key"

	SendIMFileToolName  = "feishu_send_im_file"
	SendIMImageToolName = "feishu_send_im_image"
	SendIMVideoToolName = "feishu_send_im_video"
)

type Publication struct {
	URL   string
	Token string
}

type FeishuClient interface {
	ReplyLocalAttachment(context.Context, string, string, bool) error
	ReplyLocalImage(context.Context, string, string, bool) error
	ReplyLocalVideo(context.Context, string, string, bool) error
}

type StateProvider interface {
	GetSession(string) *state.Session
	AllSessions() []*state.Session
	GetSubmission(string) *state.Submission
}

type StartedTurnItem struct {
	ThreadID string
	TurnID   string
	ItemID   string
	Type     string
	ToolName string
	Raw      map[string]any
}

type App interface {
	Feishu() FeishuClient
	State() StateProvider
	StartedTurnItems() []StartedTurnItem
	FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission)
	ReplyInThreadForSubmission(sub *state.Submission) bool
}

type Service struct {
	app App

	mu      sync.Mutex
	server  *http.Server
	handler http.Handler
	url     string
	token   string
}

type mcpJSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpJSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      json.RawMessage  `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *mcpJSONRPCError `json:"error,omitempty"`
}

type mcpJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolContext struct {
	SessionKey string
	ThreadID   string
	TurnID     string
	ItemID     string
	Submission *state.Submission
}

func NewService(a App) (*Service, error) {
	token, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	svc := &Service{app: a, token: token}
	svc.handler = http.HandlerFunc(svc.serveHTTP)
	return svc, nil
}

func (s *Service) Handler() http.Handler {
	if s == nil {
		return nil
	}
	return s.handler
}

func (s *Service) Token() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.token)
}

func (s *Service) Start(_ context.Context) error {
	if s == nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: s.handler, ReadHeaderTimeout: 5 * time.Second}
	s.mu.Lock()
	s.server = server
	s.url = "http://" + ln.Addr().String() + Path
	s.mu.Unlock()
	go func() {
		_ = server.Serve(ln)
	}()
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

func (s *Service) Publication() Publication {
	if s == nil {
		return Publication{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Publication{
		URL:   strings.TrimSpace(s.url),
		Token: strings.TrimSpace(s.token),
	}
}

func (s *Service) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackRequest(r.RemoteAddr) {
		http.Error(w, "loopback required", http.StatusForbidden)
		return
	}
	expected := strings.TrimSpace(s.Publication().Token)
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if expected == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeMCPJSONError(w, nil, -32700, "read request body failed")
		return
	}
	var req mcpJSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeMCPJSONError(w, nil, -32700, "invalid json")
		return
	}

	sessionID := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
	if sessionID == "" {
		sessionID, _ = randomToken(12)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Protocol-Version", Protocol)
	w.Header().Set("Mcp-Session-Id", sessionID)

	switch strings.TrimSpace(req.Method) {
	case "initialize":
		writeMCPJSONResult(w, req.ID, map[string]any{
			"protocolVersion": Protocol,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    ServerID,
				"version": "dev",
			},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeMCPJSONResult(w, req.ID, map[string]any{
			"tools": toolDefinitions(),
		})
	case "tools/call":
		result, callErr := s.handleToolsCall(req.Params, strings.TrimSpace(r.URL.Query().Get(SessionKeyName)))
		if callErr != nil {
			writeMCPJSONResult(w, req.ID, map[string]any{
				"content": []map[string]any{{
					"type": "text",
					"text": marshalMCPToolPayload(map[string]any{"error": map[string]any{
						"code":      callErr.Code,
						"message":   callErr.Message,
						"retryable": callErr.Retryable,
					}}),
				}},
				"structuredContent": map[string]any{"error": map[string]any{
					"code":      callErr.Code,
					"message":   callErr.Message,
					"retryable": callErr.Retryable,
				}},
				"isError": true,
			})
			return
		}
		writeMCPJSONResult(w, req.ID, result)
	default:
		writeMCPJSONError(w, req.ID, -32601, "method not found")
	}
}

type toolError struct {
	Code      string
	Message   string
	Retryable bool
}

func (s *Service) handleToolsCall(raw json.RawMessage, sessionKey string) (map[string]any, *toolError) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &toolError{Code: "invalid_params", Message: "invalid tools/call params"}
	}
	toolName := strings.TrimSpace(params.Name)
	if toolName == "" {
		return nil, &toolError{Code: "tool_required", Message: "tool name is required"}
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	ctx := s.resolveToolContext(toolName, params.Arguments, sessionKey)
	if ctx == nil || ctx.Submission == nil {
		return nil, &toolError{Code: "tool_context_not_found", Message: "could not resolve an active Feishu conversation for this MCP tool call", Retryable: true}
	}
	path := strings.TrimSpace(turnitem.StringValue(params.Arguments["path"]))
	if path == "" {
		return nil, &toolError{Code: "path_required", Message: "path is required"}
	}
	if _, err := validateToolLocalFile(path); err != nil {
		return nil, err
	}
	if s == nil || s.app == nil || s.app.Feishu() == nil {
		return nil, &toolError{Code: "send_failed", Message: "Feishu sender unavailable", Retryable: true}
	}
	inThread := s.app.ReplyInThreadForSubmission(ctx.Submission)
	switch toolName {
	case SendIMFileToolName:
		if err := s.app.Feishu().ReplyLocalAttachment(context.Background(), ctx.Submission.TriggerMessageID, path, inThread); err != nil {
			return nil, &toolError{Code: "send_failed", Message: err.Error(), Retryable: true}
		}
	case SendIMImageToolName:
		if err := s.app.Feishu().ReplyLocalImage(context.Background(), ctx.Submission.TriggerMessageID, path, inThread); err != nil {
			return nil, &toolError{Code: "send_failed", Message: err.Error(), Retryable: true}
		}
	case SendIMVideoToolName:
		if err := s.app.Feishu().ReplyLocalVideo(context.Background(), ctx.Submission.TriggerMessageID, path, inThread); err != nil {
			return nil, &toolError{Code: "send_failed", Message: err.Error(), Retryable: true}
		}
	default:
		return nil, &toolError{Code: "unknown_tool", Message: "unknown tool"}
	}
	result := map[string]any{
		"tool":        toolName,
		"path":        path,
		"file_name":   filepath.Base(path),
		"thread_id":   ctx.ThreadID,
		"turn_id":     ctx.TurnID,
		"session_key": ctx.SessionKey,
	}
	return map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": marshalMCPToolPayload(result),
		}},
		"structuredContent": result,
	}, nil
}

func (s *Service) resolveToolContext(toolName string, arguments map[string]any, sessionKey string) *toolContext {
	if s == nil || s.app == nil {
		return nil
	}
	canonicalArgs := canonicalMCPArguments(arguments)

	var match *toolContext
	requiresSessionKey := false
	for _, itemState := range s.app.StartedTurnItems() {
		item := turnitem.CloneJSONMap(itemState.Raw)
		itemType := turnitem.NormalizeTurnItemType(turnitem.FirstNonEmpty(turnitem.StringValue(item["type"]), itemState.Type))
		switch itemType {
		case "mcp_tool_call":
			if !matchesCodexMCPToolCall(item, toolName, canonicalArgs) {
				continue
			}
		case "dynamic_tool_call":
			if turnitem.ClassifyDynamicTool(strings.TrimSpace(turnitem.FirstNonEmpty(turnitem.StringValue(item["tool"]), itemState.ToolName))) != turnitem.DynamicToolMCPCategory {
				continue
			}
			if !matchesClaudeMCPToolCall(item, toolName, canonicalArgs) {
				continue
			}
			if strings.TrimSpace(sessionKey) == "" {
				requiresSessionKey = true
				continue
			}
		default:
			continue
		}
		foundSessionKey, sub := s.app.FindSubmissionByTurn(itemState.ThreadID, itemState.TurnID)
		if sub == nil {
			continue
		}
		if strings.TrimSpace(sessionKey) != "" && strings.TrimSpace(foundSessionKey) != strings.TrimSpace(sessionKey) {
			continue
		}
		if match != nil {
			return nil
		}
		match = &toolContext{
			SessionKey: foundSessionKey,
			ThreadID:   strings.TrimSpace(itemState.ThreadID),
			TurnID:     strings.TrimSpace(itemState.TurnID),
			ItemID:     strings.TrimSpace(itemState.ItemID),
			Submission: sub,
		}
	}
	if match != nil {
		return match
	}
	if requiresSessionKey {
		return nil
	}
	if strings.TrimSpace(sessionKey) != "" {
		return s.resolveToolContextFromSession(sessionKey)
	}
	return s.resolveOnlyActiveToolContext()
}

func (s *Service) resolveToolContextFromSession(sessionKey string) *toolContext {
	if s == nil || s.app == nil || s.app.State() == nil {
		return nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	sess := s.app.State().GetSession(sessionKey)
	return s.resolveToolContextFromSessionSnapshot(sess)
}

func (s *Service) resolveOnlyActiveToolContext() *toolContext {
	if s == nil || s.app == nil || s.app.State() == nil {
		return nil
	}
	var match *toolContext
	for _, sess := range s.app.State().AllSessions() {
		ctx := s.resolveToolContextFromSessionSnapshot(sess)
		if ctx == nil {
			continue
		}
		if match != nil {
			return nil
		}
		match = ctx
	}
	return match
}

func (s *Service) resolveToolContextFromSessionSnapshot(sess *state.Session) *toolContext {
	if s == nil || s.app == nil || s.app.State() == nil || sess == nil {
		return nil
	}
	sessionctx.EnsureActiveOperations(sess)
	var match *toolContext
	for _, op := range sess.ActiveOperations {
		submissionID := strings.TrimSpace(op.SubmissionID)
		if submissionID == "" {
			continue
		}
		sub := s.app.State().GetSubmission(submissionID)
		if sub == nil || sub.Finalized || state.NormalizeSubmissionStatus(sub.Status) != state.SubmissionStatusRunning {
			continue
		}
		if strings.TrimSpace(sub.TriggerMessageID) == "" {
			continue
		}
		threadID := turnitem.FirstNonEmpty(strings.TrimSpace(op.ThreadID), strings.TrimSpace(sub.ThreadID), strings.TrimSpace(sess.ActiveThreadID))
		turnID := turnitem.FirstNonEmpty(strings.TrimSpace(op.TurnID), strings.TrimSpace(sub.TurnID), strings.TrimSpace(sess.ActiveTurnID))
		ctx := &toolContext{
			SessionKey: strings.TrimSpace(sess.Key),
			ThreadID:   threadID,
			TurnID:     turnID,
			Submission: sub,
		}
		if match != nil {
			return nil
		}
		match = ctx
	}
	return match
}

func matchesCodexMCPToolCall(item map[string]any, toolName, canonicalArgs string) bool {
	if normalizeMCPToolName(turnitem.StringValue(item["tool"])) != normalizeMCPToolName(toolName) {
		return false
	}
	server := normalizeMCPServerName(turnitem.StringValue(item["server"]))
	if server != "" && server != normalizeMCPServerName(ServerID) {
		return false
	}
	return canonicalMCPArguments(turnitem.ToolCallInput(item)) == canonicalArgs
}

func matchesClaudeMCPToolCall(item map[string]any, toolName, canonicalArgs string) bool {
	return normalizeMCPToolName(turnitem.FirstNonEmpty(turnitem.StringValue(item["tool"]), turnitem.StringValue(item["toolName"]))) == normalizeMCPToolName(toolName) &&
		canonicalMCPArguments(turnitem.ToolCallInput(item)) == canonicalArgs
}

func normalizeMCPToolName(value string) string {
	value = strings.TrimSpace(turnitem.DisplayToolName(value))
	if i := strings.LastIndex(value, "/"); i >= 0 {
		value = value[i+1:]
	}
	return strings.TrimSpace(value)
}

func normalizeMCPServerName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func canonicalMCPArguments(value any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(turnitem.CloneJSONValue(value))
	if err != nil || len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func validateToolLocalFile(path string) (os.FileInfo, *toolError) {
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return nil, &toolError{Code: "file_not_found", Message: "path does not exist"}
	case err != nil:
		return nil, &toolError{Code: "file_access_failed", Message: err.Error(), Retryable: true}
	case !info.Mode().IsRegular():
		return nil, &toolError{Code: "invalid_file_path", Message: "path must point to a regular file"}
	case info.Size() <= 0:
		return nil, &toolError{Code: "empty_file", Message: "path must point to a non-empty file"}
	default:
		return info, nil
	}
}

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        SendIMFileToolName,
			Description: "Send a local file to the Feishu conversation that started the current turn as a downloadable file attachment.",
			InputSchema: singlePathToolSchema("Existing local file path to send as a Feishu file attachment"),
		},
		{
			Name:        SendIMImageToolName,
			Description: "Send a local image to the Feishu conversation that started the current turn as an inline image message.",
			InputSchema: singlePathToolSchema("Existing local image path to send as a Feishu inline image"),
		},
		{
			Name:        SendIMVideoToolName,
			Description: "Send a local .mp4 file to the Feishu conversation that started the current turn as an inline video message.",
			InputSchema: singlePathToolSchema("Existing local .mp4 path to send as a Feishu inline video"),
		},
	}
}

func singlePathToolSchema(description string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": description,
			},
		},
		"required":             []string{"path"},
		"additionalProperties": true,
	}
}

func marshalMCPToolPayload(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func writeMCPJSONResult(w http.ResponseWriter, id json.RawMessage, result any) {
	_ = json.NewEncoder(w).Encode(mcpJSONRPCResponse{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), id...),
		Result:  result,
	})
}

func writeMCPJSONError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	_ = json.NewEncoder(w).Encode(mcpJSONRPCResponse{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), id...),
		Error: &mcpJSONRPCError{
			Code:    code,
			Message: message,
		},
	})
}

func isLoopbackRequest(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func PrepareClaudeConfig(dataDir string, pub Publication, sessionKey string) (string, []string, func(), error) {
	if strings.TrimSpace(pub.URL) == "" || strings.TrimSpace(pub.Token) == "" {
		return "", nil, nil, nil
	}
	configURL, err := appendQueryParam(pub.URL, SessionKeyName, sessionKey)
	if err != nil {
		return "", nil, nil, err
	}
	path := filepath.Join(strings.TrimSpace(dataDir), "runtime", "claude-mcp-"+hashedSessionKey(sessionKey)+".json")
	payload := map[string]any{
		"mcpServers": map[string]any{
			ServerID: map[string]any{
				"type": "http",
				"url":  configURL,
				"headers": map[string]string{
					"Authorization": "Bearer ${" + BearerEnvName + "}",
				},
			},
		},
	}
	if err := writeJSONFileAtomic(path, payload, 0o600); err != nil {
		return "", nil, nil, err
	}
	cleanup := func() {
		_ = os.Remove(path)
	}
	return path, []string{BearerEnvName + "=" + strings.TrimSpace(pub.Token)}, cleanup, nil
}

func appendQueryParam(rawURL, key, value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	q := parsed.Query()
	q.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func hashedSessionKey(sessionKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionKey)))
	return hex.EncodeToString(sum[:8])
}

func writeJSONFileAtomic(path string, payload any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
