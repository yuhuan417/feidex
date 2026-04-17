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
	DataDir    string       `toml:"data_dir"`
	Log        LogConfig    `toml:"log"`
	Feishu     FeishuConfig `toml:"feishu"`
	Codex      CodexConfig  `toml:"codex"`
	Daemon     DaemonConfig `toml:"daemon"`
	Workspaces []Workspace  `toml:"workspace"`
}

type LogConfig struct {
	Level string `toml:"level"`
}

type FeishuConfig struct {
	AppID               string    `toml:"app_id"`
	AppSecret           string    `toml:"app_secret"`
	AllowFrom           []string  `toml:"allow_from"`
	DebugAllowFrom      []string  `toml:"debug_allow_from"`
	GroupAtOnly         bool      `toml:"group_at_only"`
	RespondToAtEveryone bool      `toml:"respond_to_at_everyone"`
	CardEnabled         bool      `toml:"card_enabled"`
	ReplyInThread       bool      `toml:"reply_in_thread"`
	Quiet               QuietMode `toml:"quiet"`
}

type CodexConfig struct {
	Command          string `toml:"command"`
	Transport        string `toml:"transport"`
	WSURL            string `toml:"ws_url"`
	WSBearerToken    string `toml:"ws_bearer_token"`
	ExperimentalAPI  bool   `toml:"experimental_api"`
	ServiceName      string `toml:"service_name"`
	AppServerDir     string `toml:"app_server_dir"`
	AppServerIdleTTL string `toml:"app_server_idle_ttl"`
	Model            string `toml:"model"`
	ReasoningEffort  string `toml:"reasoning_effort"`
}

type DaemonConfig struct {
	ServiceName string `toml:"service_name"`
}

type Workspace struct {
	ID             string `toml:"id"`
	Name           string `toml:"name"`
	Cwd            string `toml:"cwd"`
	AppServerDir   string `toml:"app_server_dir"`
	Model          string `toml:"model"`
	ApprovalPolicy string `toml:"approval_policy"`
	SandboxMode    string `toml:"sandbox_mode"`
}

func Default() *Config {
	return &Config{
		DataDir: ".feidex-data",
		Log: LogConfig{
			Level: "info",
		},
		Feishu: FeishuConfig{
			GroupAtOnly:   true,
			CardEnabled:   true,
			ReplyInThread: true,
			Quiet:         QuietModeVerbose,
		},
		Codex: CodexConfig{
			Command:          "codex",
			Transport:        "stdio",
			ExperimentalAPI:  true,
			ServiceName:      "feidex",
			AppServerIdleTTL: "15m",
		},
		Daemon: DaemonConfig{
			ServiceName: "feidex",
		},
		Workspaces: []Workspace{
			{
				ID:             "default",
				Name:           "Default",
				Cwd:            ".",
				Model:          "",
				ApprovalPolicy: "on-request",
				SandboxMode:    "workspace-write",
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
	c.Codex.ServiceName = strings.TrimSpace(c.Codex.ServiceName)
	if c.Codex.ServiceName == "" {
		c.Codex.ServiceName = "feidex"
	}
	c.Codex.AppServerDir = strings.TrimSpace(c.Codex.AppServerDir)
	if c.Codex.AppServerDir != "" && !filepath.IsAbs(c.Codex.AppServerDir) {
		c.Codex.AppServerDir = filepath.Clean(filepath.Join(baseDir, c.Codex.AppServerDir))
	}
	c.Codex.AppServerIdleTTL = strings.TrimSpace(c.Codex.AppServerIdleTTL)
	if c.Codex.AppServerIdleTTL == "" {
		c.Codex.AppServerIdleTTL = "15m"
	}
	if _, err := time.ParseDuration(c.Codex.AppServerIdleTTL); err != nil {
		return fmt.Errorf("invalid codex.app_server_idle_ttl %q: %w", c.Codex.AppServerIdleTTL, err)
	}
	c.Daemon.ServiceName = strings.TrimSpace(c.Daemon.ServiceName)
	if c.Daemon.ServiceName == "" {
		c.Daemon.ServiceName = "feidex"
	}
	c.Codex.Model = strings.TrimSpace(c.Codex.Model)
	c.Codex.ReasoningEffort = strings.TrimSpace(c.Codex.ReasoningEffort)
	level, err := NormalizeLogLevel(c.Log.Level)
	if err != nil {
		return err
	}
	c.Log.Level = level
	if strings.TrimSpace(c.Codex.Transport) == "" {
		if strings.TrimSpace(c.Codex.WSURL) != "" {
			c.Codex.Transport = "ws"
		} else {
			c.Codex.Transport = "stdio"
		}
	}
	quietMode, err := ParseQuietMode(c.Feishu.Quiet)
	if err != nil {
		quietMode = QuietModeNormal
	}
	c.Feishu.Quiet = quietMode
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
		ws.AppServerDir = strings.TrimSpace(ws.AppServerDir)
		if ws.AppServerDir != "" && !filepath.IsAbs(ws.AppServerDir) {
			ws.AppServerDir = filepath.Clean(filepath.Join(baseDir, ws.AppServerDir))
		}
		if ws.ApprovalPolicy == "" {
			ws.ApprovalPolicy = "on-request"
		}
		if ws.SandboxMode == "" {
			ws.SandboxMode = "workspace-write"
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

func FindWorkspace(cfg *Config, id string) *Workspace {
	for i := range cfg.Workspaces {
		if cfg.Workspaces[i].ID == id {
			return &cfg.Workspaces[i]
		}
	}
	return nil
}
