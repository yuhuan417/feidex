package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
)

type codexWorkspaceContextKey struct{}

type workspaceCodexPool struct {
	cfg     *config.Config
	factory func(config.CodexConfig) codexClient

	mu              sync.Mutex
	started         bool
	experimentalAPI bool
	onNotification  func(string, json.RawMessage)
	onRequest       func(codexrpc.RequestEnvelope)
	clients         map[string]*workspaceCodexPoolClient
	threadClients   map[string]string
	requestClients  map[string]string
}

type workspaceCodexPoolClient struct {
	key          string
	workspaceID  string
	client       codexClient
	lastUsedAt   time.Time
	boundThreads map[string]struct{}
}

func newWorkspaceCodexPool(cfg *config.Config, factory func(config.CodexConfig) codexClient) *workspaceCodexPool {
	if factory == nil {
		return nil
	}
	return &workspaceCodexPool{
		cfg:            cfg,
		factory:        factory,
		clients:        map[string]*workspaceCodexPoolClient{},
		threadClients:  map[string]string{},
		requestClients: map[string]string{},
	}
}

func withCodexWorkspace(ctx context.Context, workspaceID string) context.Context {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, codexWorkspaceContextKey{}, workspaceID)
}

func codexWorkspaceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	workspaceID, _ := ctx.Value(codexWorkspaceContextKey{}).(string)
	return strings.TrimSpace(workspaceID)
}

func (p *workspaceCodexPool) SetHandlers(onNotification func(string, json.RawMessage), onRequest func(codexrpc.RequestEnvelope)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onNotification = onNotification
	p.onRequest = onRequest
}

func (p *workspaceCodexPool) Start(ctx context.Context, experimentalAPI bool) error {
	if p == nil {
		return fmt.Errorf("codex client not initialized")
	}
	p.mu.Lock()
	p.started = true
	p.experimentalAPI = experimentalAPI
	p.mu.Unlock()
	return nil
}

func (p *workspaceCodexPool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	entries := make([]*workspaceCodexPoolClient, 0, len(p.clients))
	for _, entry := range p.clients {
		entries = append(entries, entry)
	}
	p.clients = map[string]*workspaceCodexPoolClient{}
	p.mu.Unlock()

	var firstErr error
	for _, entry := range entries {
		if entry == nil || entry.client == nil {
			continue
		}
		if err := entry.client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *workspaceCodexPool) Call(ctx context.Context, method string, params any, out any) error {
	if p == nil {
		return fmt.Errorf("codex client not initialized")
	}
	key, workspaceID, threadID := p.resolveClientRoute(ctx, params)
	entry, err := p.clientForKey(withCodexWorkspace(ctx, workspaceID), key)
	if err != nil {
		return err
	}
	if err := p.ensureThreadBound(withCodexWorkspace(ctx, workspaceID), entry, method, threadID, params); err != nil {
		return err
	}
	if err := entry.client.Call(ctx, method, params, out); err != nil {
		return err
	}
	p.recordCallSuccess(entry.key, method, params, out)
	return nil
}

func (p *workspaceCodexPool) Reply(id json.RawMessage, result any) error {
	return p.replyByRequestID(id, func(client codexClient) error {
		return client.Reply(id, result)
	})
}

func (p *workspaceCodexPool) ReplyError(id json.RawMessage, code int, message string) error {
	return p.replyByRequestID(id, func(client codexClient) error {
		return client.ReplyError(id, code, message)
	})
}

func (p *workspaceCodexPool) replyByRequestID(id json.RawMessage, fn func(codexClient) error) error {
	if p == nil {
		return fmt.Errorf("codex client not initialized")
	}
	requestKey := requestIDKey(id)
	p.mu.Lock()
	key := p.requestClients[requestKey]
	entry := p.clients[key]
	p.mu.Unlock()
	if entry == nil {
		if key == "" {
			return fmt.Errorf("no codex client route for request %s", requestKey)
		}
		var err error
		entry, err = p.clientForKey(context.Background(), key)
		if err != nil {
			return err
		}
	}
	if err := fn(entry.client); err != nil {
		return err
	}
	p.touchClient(entry.key)
	return nil
}

func (p *workspaceCodexPool) CloseIdleClients(now time.Time, ttl time.Duration, busyWorkspaceIDs map[string]struct{}) int {
	if p == nil || ttl <= 0 {
		return 0
	}
	p.mu.Lock()
	entries := make([]*workspaceCodexPoolClient, 0, len(p.clients))
	for key, entry := range p.clients {
		if entry == nil || entry.client == nil {
			continue
		}
		if _, busy := busyWorkspaceIDs[entry.workspaceID]; busy {
			continue
		}
		if p.clientHasOpenRequestsLocked(key) {
			continue
		}
		if entry.lastUsedAt.IsZero() || now.Sub(entry.lastUsedAt) < ttl {
			continue
		}
		delete(p.clients, key)
		entries = append(entries, entry)
	}
	p.mu.Unlock()

	closed := 0
	for _, entry := range entries {
		if err := entry.client.Close(); err != nil {
			slog.Warn("idle codex app-server close failed",
				"workspace_id", entry.workspaceID,
				"error", err,
			)
			continue
		}
		closed++
	}
	return closed
}

func (p *workspaceCodexPool) resolveClientRoute(ctx context.Context, params any) (key string, workspaceID string, threadID string) {
	if p == nil {
		return "", "", ""
	}
	threadID = routeThreadIDFromParams(params)
	if threadID != "" {
		p.mu.Lock()
		key = p.threadClients[threadID]
		p.mu.Unlock()
		if key != "" {
			return key, key, threadID
		}
	}
	workspaceID = codexWorkspaceFromContext(ctx)
	if workspaceID == "" {
		workspaceID = p.workspaceIDForCWD(routeCWDFromParams(params))
	}
	if workspaceID == "" {
		workspaceID = p.defaultWorkspaceID()
	}
	return workspaceID, workspaceID, threadID
}

func (p *workspaceCodexPool) clientForKey(ctx context.Context, key string) (*workspaceCodexPoolClient, error) {
	if key == "" {
		key = p.defaultWorkspaceID()
	}
	p.mu.Lock()
	if entry := p.clients[key]; entry != nil {
		entry.lastUsedAt = time.Now()
		p.mu.Unlock()
		return entry, nil
	}
	started := p.started
	experimentalAPI := p.experimentalAPI
	cfg := p.codexConfigForKeyLocked(key)
	entry := &workspaceCodexPoolClient{
		key:          key,
		workspaceID:  p.workspaceIDForKeyLocked(key),
		client:       p.factory(cfg),
		lastUsedAt:   time.Now(),
		boundThreads: map[string]struct{}{},
	}
	p.installHandlersLocked(entry)
	p.clients[key] = entry
	p.mu.Unlock()

	if started {
		if err := entry.client.Start(ctx, experimentalAPI); err != nil {
			p.mu.Lock()
			if current := p.clients[key]; current == entry {
				delete(p.clients, key)
			}
			p.mu.Unlock()
			return nil, err
		}
	}
	return entry, nil
}

func (p *workspaceCodexPool) installHandlersLocked(entry *workspaceCodexPoolClient) {
	if entry == nil || entry.client == nil {
		return
	}
	entry.client.SetHandlers(func(method string, params json.RawMessage) {
		p.recordNotification(entry.key, method, params)
		p.mu.Lock()
		handler := p.onNotification
		p.mu.Unlock()
		if handler != nil {
			handler(method, params)
		}
	}, func(req codexrpc.RequestEnvelope) {
		p.recordRequest(entry.key, req)
		p.mu.Lock()
		handler := p.onRequest
		p.mu.Unlock()
		if handler != nil {
			handler(req)
		}
	})
}

func (p *workspaceCodexPool) ensureThreadBound(ctx context.Context, entry *workspaceCodexPoolClient, method, threadID string, params any) error {
	threadID = strings.TrimSpace(threadID)
	if entry == nil || entry.client == nil || threadID == "" {
		return nil
	}
	switch strings.TrimSpace(method) {
	case "thread/start", "thread/resume", "thread/list":
		return nil
	}

	p.mu.Lock()
	_, bound := entry.boundThreads[threadID]
	p.mu.Unlock()
	if bound {
		return nil
	}

	resumeParams := map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}
	if model := configuredGlobalModel(p.cfg); strings.TrimSpace(model) != "" {
		resumeParams["model"] = strings.TrimSpace(model)
	}
	var result codexrpc.ThreadStartResult
	if err := entry.client.Call(ctx, "thread/resume", resumeParams, &result); err != nil {
		return err
	}
	p.recordThreadRoute(entry.key, firstNonEmpty(strings.TrimSpace(result.Thread.ID), threadID))
	return nil
}

func (p *workspaceCodexPool) recordCallSuccess(key, method string, params any, out any) {
	p.touchClient(key)
	threadID := routeThreadIDFromParams(params)
	switch strings.TrimSpace(method) {
	case "thread/start", "thread/resume", "thread/fork":
		if result, ok := out.(*codexrpc.ThreadStartResult); ok && result != nil {
			threadID = firstNonEmpty(strings.TrimSpace(result.Thread.ID), threadID)
		}
	}
	if threadID != "" {
		p.recordThreadRoute(key, threadID)
	}
}

func (p *workspaceCodexPool) recordNotification(key, method string, params json.RawMessage) {
	p.touchClient(key)
	var header struct {
		ThreadID  string          `json:"threadId"`
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := json.Unmarshal(params, &header); err == nil {
		if strings.TrimSpace(header.ThreadID) != "" {
			p.recordThreadRoute(key, header.ThreadID)
		}
		if strings.TrimSpace(method) == "serverRequest/resolved" {
			p.mu.Lock()
			delete(p.requestClients, requestIDKey(header.RequestID))
			p.mu.Unlock()
		}
	}
}

func (p *workspaceCodexPool) recordRequest(key string, req codexrpc.RequestEnvelope) {
	p.touchClient(key)
	p.mu.Lock()
	p.requestClients[requestIDKey(req.ID)] = key
	p.mu.Unlock()
	var header struct {
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(req.Params, &header); err == nil && strings.TrimSpace(header.ThreadID) != "" {
		p.recordThreadRoute(key, header.ThreadID)
	}
}

func (p *workspaceCodexPool) recordThreadRoute(key, threadID string) {
	key = strings.TrimSpace(key)
	threadID = strings.TrimSpace(threadID)
	if key == "" || threadID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.threadClients[threadID] = key
	entry := p.clients[key]
	if entry == nil {
		return
	}
	entry.lastUsedAt = time.Now()
	if entry.boundThreads == nil {
		entry.boundThreads = map[string]struct{}{}
	}
	entry.boundThreads[threadID] = struct{}{}
}

func (p *workspaceCodexPool) touchClient(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.clients[strings.TrimSpace(key)]; entry != nil {
		entry.lastUsedAt = time.Now()
	}
}

func (p *workspaceCodexPool) workspaceIDForCWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" || p == nil || p.cfg == nil {
		return ""
	}
	for _, ws := range p.cfg.Workspaces {
		if sameWorkspaceCWD(cwd, ws.Cwd) {
			return strings.TrimSpace(ws.ID)
		}
	}
	return ""
}

func (p *workspaceCodexPool) defaultWorkspaceID() string {
	if p == nil || p.cfg == nil || len(p.cfg.Workspaces) == 0 {
		return "default"
	}
	return strings.TrimSpace(p.cfg.Workspaces[0].ID)
}

func (p *workspaceCodexPool) codexConfigForKeyLocked(key string) config.CodexConfig {
	cfg := p.cfg.Codex
	workspaceID := p.workspaceIDForKeyLocked(key)
	cfg.AppServerDir = p.appServerDirForWorkspaceLocked(workspaceID)
	return cfg
}

func (p *workspaceCodexPool) workspaceIDForKeyLocked(key string) string {
	if strings.TrimSpace(key) != "" {
		return strings.TrimSpace(key)
	}
	return p.defaultWorkspaceID()
}

func (p *workspaceCodexPool) appServerDirForWorkspaceLocked(workspaceID string) string {
	if p == nil || p.cfg == nil {
		return ""
	}
	if ws := config.FindWorkspace(p.cfg, workspaceID); ws != nil {
		if dir := strings.TrimSpace(ws.AppServerDir); dir != "" {
			return dir
		}
		if dir := strings.TrimSpace(p.cfg.Codex.AppServerDir); dir != "" {
			return dir
		}
		return strings.TrimSpace(ws.Cwd)
	}
	return strings.TrimSpace(p.cfg.Codex.AppServerDir)
}

func (p *workspaceCodexPool) clientHasOpenRequestsLocked(key string) bool {
	for _, requestKey := range p.requestClients {
		if requestKey == key {
			return true
		}
	}
	return false
}

func routeThreadIDFromParams(params any) string {
	values, _ := params.(map[string]any)
	return strings.TrimSpace(stringValue(values["threadId"]))
}

func routeCWDFromParams(params any) string {
	values, _ := params.(map[string]any)
	return strings.TrimSpace(stringValue(values["cwd"]))
}
