package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const DefaultShutdownTimeout = 10 * time.Second

type Config struct {
	DataDir    string       `toml:"data_dir"`
	Feishu     FeishuConfig `toml:"feishu"`
	Codex      CodexConfig  `toml:"codex"`
	Workspaces []Workspace  `toml:"workspace"`
}

type FeishuConfig struct {
	AppID               string   `toml:"app_id"`
	AppSecret           string   `toml:"app_secret"`
	AllowFrom           []string `toml:"allow_from"`
	GroupAtOnly         bool     `toml:"group_at_only"`
	RespondToAtEveryone bool     `toml:"respond_to_at_everyone"`
	CardEnabled         bool     `toml:"card_enabled"`
	ReplyInThread       bool     `toml:"reply_in_thread"`
}

type CodexConfig struct {
	Command         string `toml:"command"`
	Transport       string `toml:"transport"`
	WSURL           string `toml:"ws_url"`
	WSBearerToken   string `toml:"ws_bearer_token"`
	ExperimentalAPI bool   `toml:"experimental_api"`
	ServiceName     string `toml:"service_name"`
}

type Workspace struct {
	ID             string `toml:"id"`
	Name           string `toml:"name"`
	Cwd            string `toml:"cwd"`
	Model          string `toml:"model"`
	ApprovalPolicy string `toml:"approval_policy"`
	SandboxMode    string `toml:"sandbox_mode"`
}

func Default() *Config {
	return &Config{
		DataDir: ".feidex-data",
		Feishu: FeishuConfig{
			GroupAtOnly:   true,
			CardEnabled:   true,
			ReplyInThread: true,
		},
		Codex: CodexConfig{
			Command:         "codex",
			Transport:       "stdio",
			ExperimentalAPI: true,
			ServiceName:     "feidex",
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
	if strings.TrimSpace(c.Codex.Transport) == "" {
		if strings.TrimSpace(c.Codex.WSURL) != "" {
			c.Codex.Transport = "ws"
		} else {
			c.Codex.Transport = "stdio"
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

func Save(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

func FindWorkspace(cfg *Config, id string) *Workspace {
	for i := range cfg.Workspaces {
		if cfg.Workspaces[i].ID == id {
			return &cfg.Workspaces[i]
		}
	}
	return nil
}
