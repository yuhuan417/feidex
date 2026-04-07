package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

const ServiceName = "feidex"

type Config struct {
	BinaryPath string
	ConfigPath string
	WorkDir    string
	EnvPATH    string
	HomeDir    string
}

type Status struct {
	Installed bool
	Running   bool
	PID       int
	Platform  string
	UnitPath  string
}

type Manager interface {
	Install(cfg Config) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (*Status, error)
	Platform() string
}

func NewManager() (Manager, error) {
	return newPlatformManager()
}

func Resolve(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("nil daemon config")
	}
	if cfg.BinaryPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("detect binary path: %w", err)
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		cfg.BinaryPath = exe
	}
	cfg.BinaryPath = filepath.Clean(cfg.BinaryPath)
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = "config.toml"
	}
	absConfig, err := filepath.Abs(cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg.ConfigPath = filepath.Clean(absConfig)
	if cfg.WorkDir == "" {
		cfg.WorkDir = filepath.Dir(cfg.ConfigPath)
	}
	cfg.WorkDir = filepath.Clean(cfg.WorkDir)
	if cfg.EnvPATH == "" {
		cfg.EnvPATH = os.Getenv("PATH")
	}
	if cfg.HomeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("detect home directory: %w", err)
		}
		cfg.HomeDir = home
	}
	cfg.HomeDir = filepath.Clean(cfg.HomeDir)
	return nil
}
