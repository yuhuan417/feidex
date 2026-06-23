package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const DefaultShutdownTimeout = 10 * time.Second

type Config struct {
	DataDir    string           `toml:"data_dir"`
	Log        LogConfig        `toml:"log"`
	Feishu     FeishuConfig     `toml:"feishu"`
	Frontends  []FrontendConfig `toml:"frontend"`
	Codex      CodexConfig      `toml:"codex"`
	Claude     ClaudeConfig     `toml:"claude"`
	Daemon     DaemonConfig     `toml:"daemon"`
	Workspaces []Workspace      `toml:"workspace"`
}

type LogConfig struct {
	Level string `toml:"level"`
}

type FeishuConfig struct {
	Backend             string    `toml:"backend"`
	AutoRetry           bool      `toml:"codex_auto_retry"`
	AppID               string    `toml:"app_id"`
	AppSecret           string    `toml:"app_secret"`
	Domain              string    `toml:"domain"`
	AllowFrom           []string  `toml:"allow_from"`
	DebugAllowFrom      []string  `toml:"debug_allow_from"`
	GroupAtOnly         bool      `toml:"group_at_only"`
	RespondToAtEveryone bool      `toml:"respond_to_at_everyone"`
	CardEnabled         bool      `toml:"card_enabled"`
	ReplyInThread       bool      `toml:"reply_in_thread"`
	Quiet               QuietMode `toml:"quiet"`
}

type FrontendConfig struct {
	ID string `toml:"id"`
	FeishuConfig
}

type ResolvedFrontend struct {
	ID          string
	Backend     string
	Feishu      FeishuConfig
	ConfigIndex int
}

type CodexConfig struct {
	Command             string `toml:"command"`
	Transport           string `toml:"transport"`
	WSURL               string `toml:"ws_url"`
	WSBearerToken       string `toml:"ws_bearer_token"`
	ExperimentalAPI     bool   `toml:"experimental_api"`
	ServiceName         string `toml:"service_name"`
	AppServerDir        string `toml:"app_server_dir"`
	Model               string `toml:"model"`
	ReasoningEffort     string `toml:"reasoning_effort"`
	PlanModel           string `toml:"plan_model"`
	PlanReasoningEffort string `toml:"plan_reasoning_effort"`
}

type ClaudeConfig struct {
	Command                    string   `toml:"command"`
	Model                      string   `toml:"model"`
	ModelOptions               []string `toml:"model_options"`
	Effort                     string   `toml:"effort"`
	PermissionMode             string   `toml:"permission_mode"`
	DangerouslySkipPermissions bool     `toml:"dangerously_skip_permissions"`
	DisablePlugins             bool     `toml:"disable_plugins"`
	SystemPrompt               string   `toml:"system_prompt"`
	PermissionPromptToolStdio  bool     `toml:"permission_prompt_tool_stdio"`
}

type DaemonConfig struct {
	ServiceName string `toml:"service_name"`
}

type Workspace struct {
	ID                   string `toml:"id"`
	Name                 string `toml:"name"`
	Cwd                  string `toml:"cwd"`
	Model                string `toml:"model"`
	ApprovalPolicy       string `toml:"approval_policy"`
	SandboxMode          string `toml:"sandbox_mode"`
	MultiAgentMode       string `toml:"multi_agent_mode"`
	ClaudePermissionMode string `toml:"claude_permission_mode"`
}

const (
	RuntimeBackendCodex  = "codex"
	RuntimeBackendClaude = "claude"
	DefaultFrontendID    = "default"
)

// Feishu/Lark Open Platform domains. The Go SDK is shared between the two
// products; only the Open Platform base URL differs.
const (
	FeishuDomainFeishu = "feishu"
	FeishuDomainLark   = "lark"

	feishuOpenBaseURL = "https://open.feishu.cn"
	larkOpenBaseURL   = "https://open.larksuite.com"
)

// OpenBaseURL returns the Open Platform base URL for the configured domain.
// Defaults to Feishu (open.feishu.cn) when domain is unset.
func (c FeishuConfig) OpenBaseURL() string {
	if c.Domain == FeishuDomainLark {
		return larkOpenBaseURL
	}
	return feishuOpenBaseURL
}

func Default() *Config {
	return &Config{
		DataDir: ".feidex-data",
		Log: LogConfig{
			Level: "info",
		},
		Feishu: defaultFeishuConfig(),
		Codex: CodexConfig{
			Command:         "codex",
			Transport:       "stdio",
			ExperimentalAPI: true,
			ServiceName:     "feidex",
		},
		Claude: ClaudeConfig{
			Command:                    "claude",
			Model:                      "sonnet",
			PermissionMode:             "default",
			DangerouslySkipPermissions: true,
			PermissionPromptToolStdio:  true,
		},
		Daemon: DaemonConfig{
			ServiceName: "feidex",
		},
		Workspaces: []Workspace{
			{
				ID:                   "default",
				Name:                 "Default",
				Cwd:                  ".",
				Model:                "",
				ApprovalPolicy:       "on-request",
				SandboxMode:          "workspace-write",
				ClaudePermissionMode: "",
			},
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("config file %q does not exist", path)
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	if err := cfg.Normalize(filepath.Dir(path)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Normalize(baseDir string) error {
	if c.Codex.Command == "" {
		c.Codex.Command = "codex"
	}
	c.Codex.Transport = strings.TrimSpace(c.Codex.Transport)
	c.Codex.WSURL = strings.TrimSpace(c.Codex.WSURL)
	c.Codex.WSBearerToken = strings.TrimSpace(c.Codex.WSBearerToken)
	c.Codex.ServiceName = strings.TrimSpace(c.Codex.ServiceName)
	if c.Codex.ServiceName == "" {
		c.Codex.ServiceName = "feidex"
	}
	c.Codex.AppServerDir = strings.TrimSpace(c.Codex.AppServerDir)
	if c.Codex.AppServerDir != "" && !filepath.IsAbs(c.Codex.AppServerDir) {
		c.Codex.AppServerDir = filepath.Clean(filepath.Join(baseDir, c.Codex.AppServerDir))
	}
	c.Daemon.ServiceName = strings.TrimSpace(c.Daemon.ServiceName)
	if c.Daemon.ServiceName == "" {
		c.Daemon.ServiceName = "feidex"
	}
	c.Codex.Model = strings.TrimSpace(c.Codex.Model)
	c.Codex.ReasoningEffort = strings.TrimSpace(c.Codex.ReasoningEffort)
	c.Codex.PlanModel = strings.TrimSpace(c.Codex.PlanModel)
	c.Codex.PlanReasoningEffort = strings.TrimSpace(c.Codex.PlanReasoningEffort)
	c.Claude.Command = strings.TrimSpace(c.Claude.Command)
	if c.Claude.Command == "" {
		c.Claude.Command = "claude"
	}
	c.Claude.Model = strings.TrimSpace(c.Claude.Model)
	if c.Claude.Model == "" {
		c.Claude.Model = "sonnet"
	}
	c.Claude.ModelOptions = normalizeStringList(c.Claude.ModelOptions)
	claudeEffort, err := NormalizeClaudeEffort(c.Claude.Effort)
	if err != nil {
		return err
	}
	c.Claude.Effort = claudeEffort
	c.Claude.PermissionMode = normalizeClaudePermissionMode(c.Claude.PermissionMode)
	if !isSupportedClaudePermissionMode(c.Claude.PermissionMode) {
		return fmt.Errorf("unsupported claude.permission_mode %q", c.Claude.PermissionMode)
	}
	if c.Claude.PermissionMode == "bypassPermissions" && !c.Claude.DangerouslySkipPermissions {
		return errors.New("claude.permission_mode=bypassPermissions requires claude.dangerously_skip_permissions = true")
	}
	c.Claude.SystemPrompt = strings.TrimSpace(c.Claude.SystemPrompt)
	level, err := NormalizeLogLevel(c.Log.Level)
	if err != nil {
		return err
	}
	c.Log.Level = level
	switch {
	case c.Codex.WSURL != "" || c.Codex.WSBearerToken != "":
		return errors.New("codex websocket transport has been removed; use stdio only")
	case c.Codex.Transport == "":
		c.Codex.Transport = "stdio"
	case strings.EqualFold(c.Codex.Transport, "ws"):
		return errors.New("codex websocket transport has been removed; use stdio only")
	case strings.EqualFold(c.Codex.Transport, "stdio"):
		c.Codex.Transport = "stdio"
	default:
		return fmt.Errorf("unsupported codex.transport %q; only stdio is supported", c.Codex.Transport)
	}
	if err := normalizeFeishuConfig(&c.Feishu); err != nil {
		return err
	}
	seenFrontends := map[string]struct{}{}
	for i := range c.Frontends {
		frontend := &c.Frontends[i]
		frontend.ID = strings.TrimSpace(frontend.ID)
		if frontend.ID == "" {
			return errors.New("frontend.id is required")
		}
		if strings.Contains(frontend.ID, ":") {
			return fmt.Errorf("frontend %q id must not contain ':'", frontend.ID)
		}
		if _, ok := seenFrontends[frontend.ID]; ok {
			return fmt.Errorf("duplicate frontend id %q", frontend.ID)
		}
		seenFrontends[frontend.ID] = struct{}{}
		frontend.FeishuConfig.Backend = normalizeBackendName(frontend.FeishuConfig.Backend)
		if err := normalizeFeishuConfig(&frontend.FeishuConfig); err != nil {
			return fmt.Errorf("frontend %q: %w", frontend.ID, err)
		}
	}
	if len(c.Workspaces) == 0 {
		return errors.New("at least one [[workspace]] is required")
	}
	seen := map[string]struct{}{}
	for i := range c.Workspaces {
		ws := &c.Workspaces[i]
		ws.ID = strings.TrimSpace(ws.ID)
		ws.Name = strings.TrimSpace(ws.Name)
		ws.Cwd = strings.TrimSpace(ws.Cwd)
		if ws.ID == "" {
			return errors.New("workspace.id is required")
		}
		if ws.Name == "" {
			ws.Name = ws.ID
		}
		if ws.Cwd == "" {
			return fmt.Errorf("workspace %q cwd is required", ws.ID)
		}
		if !filepath.IsAbs(ws.Cwd) {
			ws.Cwd = filepath.Clean(filepath.Join(baseDir, ws.Cwd))
		}
		if ws.ApprovalPolicy == "" {
			ws.ApprovalPolicy = "on-request"
		}
		if ws.SandboxMode == "" {
			ws.SandboxMode = "workspace-write"
		}
		if ws.MultiAgentMode == "" {
			ws.MultiAgentMode = "explicitRequestOnly"
		}
		ws.ClaudePermissionMode = normalizeOptionalClaudePermissionMode(ws.ClaudePermissionMode)
		if ws.ClaudePermissionMode != "" && !isSupportedClaudePermissionMode(ws.ClaudePermissionMode) {
			return fmt.Errorf("workspace %q has unsupported claude_permission_mode %q", ws.ID, ws.ClaudePermissionMode)
		}
		if ws.ClaudePermissionMode == "bypassPermissions" && !c.Claude.DangerouslySkipPermissions {
			return fmt.Errorf("workspace %q claude_permission_mode=bypassPermissions requires claude.dangerously_skip_permissions = true", ws.ID)
		}
		if _, ok := seen[ws.ID]; ok {
			return fmt.Errorf("duplicate workspace id %q", ws.ID)
		}
		seen[ws.ID] = struct{}{}
	}
	if c.DataDir != "" && !filepath.IsAbs(c.DataDir) {
		c.DataDir = filepath.Clean(filepath.Join(baseDir, c.DataDir))
	}
	return nil
}

func defaultFeishuConfig() FeishuConfig {
	return FeishuConfig{
		GroupAtOnly:   true,
		CardEnabled:   true,
		ReplyInThread: true,
		Quiet:         QuietModeProgress,
	}
}

func normalizeFeishuConfig(cfg *FeishuConfig) error {
	if cfg == nil {
		return nil
	}
	cfg.Backend = normalizeBackendName(cfg.Backend)
	switch cfg.Backend {
	case "", RuntimeBackendCodex, RuntimeBackendClaude:
	default:
		return fmt.Errorf("unsupported feishu.backend %q; must be unset, %q, or %q", cfg.Backend, RuntimeBackendCodex, RuntimeBackendClaude)
	}
	domain, err := normalizeFeishuDomain(cfg.Domain)
	if err != nil {
		return err
	}
	cfg.Domain = domain
	quietMode, err := ParseQuietMode(cfg.Quiet)
	if err != nil {
		quietMode = QuietModeNormal
	}
	cfg.Quiet = quietMode
	return nil
}

// normalizeFeishuDomain canonicalizes a domain value to FeishuDomainFeishu or
// FeishuDomainLark. An empty value defaults to Feishu.
func normalizeFeishuDomain(domain string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "", FeishuDomainFeishu:
		return FeishuDomainFeishu, nil
	case FeishuDomainLark:
		return FeishuDomainLark, nil
	default:
		return "", fmt.Errorf("unsupported feishu.domain %q; must be unset, %q, or %q", domain, FeishuDomainFeishu, FeishuDomainLark)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Config) ResolvedFrontends() []ResolvedFrontend {
	if c == nil {
		return nil
	}
	if len(c.Frontends) == 0 {
		feishuCfg := c.Feishu
		return []ResolvedFrontend{{
			ID:          DefaultFrontendID,
			Backend:     normalizeBackendName(feishuCfg.Backend),
			Feishu:      feishuCfg,
			ConfigIndex: -1,
		}}
	}
	out := make([]ResolvedFrontend, 0, len(c.Frontends))
	for i := range c.Frontends {
		frontend := c.Frontends[i]
		frontend.FeishuConfig.Backend = normalizeBackendName(frontend.FeishuConfig.Backend)
		out = append(out, ResolvedFrontend{
			ID:          strings.TrimSpace(frontend.ID),
			Backend:     frontend.FeishuConfig.Backend,
			Feishu:      frontend.FeishuConfig,
			ConfigIndex: i,
		})
	}
	return out
}

func NormalizeLogLevel(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "info", nil
	case "debug", "info", "warn", "error":
		return value, nil
	case "warning":
		return "warn", nil
	default:
		return "", fmt.Errorf("unsupported log.level %q", value)
	}
}

func ParseLogLevel(value string) (slog.Level, error) {
	normalized, err := NormalizeLogLevel(value)
	if err != nil {
		return slog.LevelInfo, err
	}
	switch normalized {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, nil
	}
}

func Save(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func FindWorkspace(cfg *Config, id string) *Workspace {
	for i := range cfg.Workspaces {
		if cfg.Workspaces[i].ID == id {
			return &cfg.Workspaces[i]
		}
	}
	return nil
}

func normalizeBackendName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case RuntimeBackendCodex:
		return RuntimeBackendCodex
	case RuntimeBackendClaude:
		return RuntimeBackendClaude
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeClaudePermissionMode(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return "default"
	case "acceptEdits", "plan", "bypassPermissions":
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(value)
	}
}

func isSupportedClaudePermissionMode(value string) bool {
	switch strings.TrimSpace(value) {
	case "default", "acceptEdits", "plan", "bypassPermissions":
		return true
	default:
		return false
	}
}

func normalizeOptionalClaudePermissionMode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return normalizeClaudePermissionMode(value)
}

var supportedClaudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

func SupportedClaudeEfforts() []string {
	out := make([]string, len(supportedClaudeEfforts))
	copy(out, supportedClaudeEfforts)
	return out
}

func NormalizeClaudeEffort(value string) (string, error) {
	raw := strings.TrimSpace(value)
	switch strings.ToLower(raw) {
	case "", "default", "auto":
		return "", nil
	case "low", "medium", "high", "xhigh", "max":
		return strings.ToLower(raw), nil
	default:
		return "", fmt.Errorf("unsupported claude.effort %q", raw)
	}
}
