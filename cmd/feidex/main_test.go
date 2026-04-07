package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"feidex/internal/app"
	"feidex/internal/config"
	"feidex/internal/daemon"
)

type fakeService struct {
	startErr error
	stopErr  error
	started  bool
	stopped  bool
}

func (f *fakeService) Start(context.Context) error {
	f.started = true
	return f.startErr
}

func (f *fakeService) Stop(context.Context) error {
	f.stopped = true
	return f.stopErr
}

type fakeManager struct {
	statusResp *daemon.Status
	statusErr  error
	installErr error
	startErr   error
	stopErr    error
	restartErr error
	removeErr  error
	platform   string
	installCfg daemon.Config
	calls      []string
}

func (f *fakeManager) Install(cfg daemon.Config) error {
	f.calls = append(f.calls, "install")
	f.installCfg = cfg
	return f.installErr
}

func (f *fakeManager) Uninstall() error {
	f.calls = append(f.calls, "uninstall")
	return f.removeErr
}

func (f *fakeManager) Start() error {
	f.calls = append(f.calls, "start")
	return f.startErr
}

func (f *fakeManager) Stop() error {
	f.calls = append(f.calls, "stop")
	return f.stopErr
}

func (f *fakeManager) Restart() error {
	f.calls = append(f.calls, "restart")
	return f.restartErr
}

func (f *fakeManager) Status() (*daemon.Status, error) {
	f.calls = append(f.calls, "status")
	return f.statusResp, f.statusErr
}

func (f *fakeManager) Platform() string {
	if f.platform != "" {
		return f.platform
	}
	return "test"
}

func withCapturedOutput(t *testing.T, fn func()) (stdout string, stderr string) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdout): %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stderr): %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW

	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&outBuf, outR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&errBuf, errR)
	}()

	fn()

	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	return outBuf.String(), errBuf.String()
}

func minimalConfig() *config.Config {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = os.TempDir()
	return cfg
}

func resetMainStubs() {
	loadConfig = config.Load
	newApp = func(cfg *config.Config, cfgPath string) (appService, error) { return app.New(cfg, cfgPath) }
	notifyCtx = signalNotifyContext
	resolveDaemonConfig = daemon.Resolve
	enableLingerUser = daemon.EnableLingerCurrentUser
	newDaemonManager = daemon.NewManager
	setupFeishu = config.SetupFeishu
}

func signalNotifyContext(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func TestRunUsageAndVersionCommands(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	stdout, _ := withCapturedOutput(t, func() {
		if got := run([]string{"version"}); got != 0 {
			t.Fatalf("run(version) = %d, want 0", got)
		}
	})
	if !strings.Contains(stdout, "feidex 0.1.0") {
		t.Fatalf("version output = %q, want version string", stdout)
	}

	stdout, _ = withCapturedOutput(t, func() {
		if got := run([]string{"help"}); got != 0 {
			t.Fatalf("run(help) = %d, want 0", got)
		}
	})
	if !strings.Contains(stdout, "Usage:") {
		t.Fatalf("help output = %q, want usage", stdout)
	}

	stdout, _ = withCapturedOutput(t, func() {
		if got := run([]string{"unknown"}); got != 1 {
			t.Fatalf("run(unknown) = %d, want 1", got)
		}
	})
	if !strings.Contains(stdout, "feidex serve") {
		t.Fatalf("unknown command output = %q, want usage", stdout)
	}
}

func TestRunServeCoversSuccessAndFailures(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	loadConfig = func(string) (*config.Config, error) {
		return nil, errors.New("boom")
	}
	_, stderr := withCapturedOutput(t, func() {
		if got := runServe(nil); got != 1 {
			t.Fatalf("runServe(load error) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "load config: boom") {
		t.Fatalf("stderr = %q, want load config error", stderr)
	}

	loadConfig = func(string) (*config.Config, error) {
		cfg := minimalConfig()
		cfg.Log.Level = "trace"
		return cfg, nil
	}
	_, stderr = withCapturedOutput(t, func() {
		if got := runServe(nil); got != 1 {
			t.Fatalf("runServe(log error) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "invalid log level") {
		t.Fatalf("stderr = %q, want log level error", stderr)
	}

	cfg := minimalConfig()
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	newApp = func(*config.Config, string) (appService, error) {
		return nil, errors.New("init failed")
	}
	_, stderr = withCapturedOutput(t, func() {
		if got := runServe([]string{"--config", "custom.toml"}); got != 1 {
			t.Fatalf("runServe(init error) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "init service: init failed") {
		t.Fatalf("stderr = %q, want init service error", stderr)
	}

	failingStart := &fakeService{startErr: errors.New("start failed")}
	newApp = func(*config.Config, string) (appService, error) { return failingStart, nil }
	notifyCtx = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}
	_, stderr = withCapturedOutput(t, func() {
		if got := runServe(nil); got != 1 {
			t.Fatalf("runServe(start error) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "start service: start failed") || !failingStart.started {
		t.Fatalf("unexpected start failure output: %q", stderr)
	}

	stopFail := &fakeService{stopErr: errors.New("stop failed")}
	newApp = func(*config.Config, string) (appService, error) { return stopFail, nil }
	notifyCtx = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, func() {}
	}
	_, stderr = withCapturedOutput(t, func() {
		if got := runServe(nil); got != 1 {
			t.Fatalf("runServe(stop error) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "stop service: stop failed") || !stopFail.stopped {
		t.Fatalf("unexpected stop failure output: %q", stderr)
	}

	success := &fakeService{}
	loadConfig = func(string) (*config.Config, error) {
		cfg := minimalConfig()
		cfg.DataDir = ""
		return cfg, nil
	}
	newApp = func(*config.Config, string) (appService, error) { return success, nil }
	notifyCtx = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, func() {}
	}
	if got := runServe(nil); got != 0 {
		t.Fatalf("runServe(success) = %d, want 0", got)
	}
	if !success.started || !success.stopped {
		t.Fatalf("expected service to start and stop, got %+v", success)
	}
}

func TestRunFeishuAndSetup(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	stdout, stderr := withCapturedOutput(t, func() {
		if got := runFeishu(nil); got != 0 {
			t.Fatalf("runFeishu(no args) = %d, want 0", got)
		}
	})
	if stderr != "" || !strings.Contains(stdout, "feidex feishu setup") {
		t.Fatalf("runFeishu(no args) output = %q / %q", stdout, stderr)
	}

	var capturedMode config.FeishuSetupMode
	var capturedOpts config.FeishuSetupOptions
	setupFeishu = func(mode config.FeishuSetupMode, opts config.FeishuSetupOptions) error {
		capturedMode = mode
		capturedOpts = opts
		return nil
	}
	if got := runFeishu([]string{"bind", "--config", "cfg.toml", "--workspace", "ws", "--app", "id:secret", "--app-id", "id", "--app-secret", "secret", "--timeout", "12", "--qr-image", "qr.png"}); got != 0 {
		t.Fatalf("runFeishu(bind) = %d, want 0", got)
	}
	if capturedMode != config.FeishuSetupBind || capturedOpts.ConfigPath != "cfg.toml" || capturedOpts.Timeout.Seconds() != 12 {
		t.Fatalf("runFeishu(bind) captured = mode=%q opts=%+v", capturedMode, capturedOpts)
	}

	setupFeishu = func(mode config.FeishuSetupMode, opts config.FeishuSetupOptions) error {
		return errors.New("setup failed")
	}
	_, stderr = withCapturedOutput(t, func() {
		if got := runFeishuSetup(config.FeishuSetupNew, nil); got != 1 {
			t.Fatalf("runFeishuSetup(error) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "feishu setup failed: setup failed") {
		t.Fatalf("stderr = %q, want setup error", stderr)
	}

	stdout, _ = withCapturedOutput(t, func() {
		if got := runFeishu([]string{"help"}); got != 0 {
			t.Fatalf("runFeishu(help) = %d, want 0", got)
		}
	})
	if !strings.Contains(stdout, "app_id:app_secret") {
		t.Fatalf("help output = %q, want usage", stdout)
	}
}

func TestRunDaemonCommandsAndRequireInstalled(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	stdout, stderr := withCapturedOutput(t, func() {
		if got := runDaemon(nil); got != 1 {
			t.Fatalf("runDaemon(no args) = %d, want 1", got)
		}
	})
	if stderr != "" || !strings.Contains(stdout, "feidex daemon install") {
		t.Fatalf("runDaemon(no args) output = %q / %q", stdout, stderr)
	}

	stdout, stderr = withCapturedOutput(t, func() {
		if got := runDaemon([]string{"help"}); got != 0 {
			t.Fatalf("runDaemon(help) = %d, want 0", got)
		}
	})
	if stderr != "" || !strings.Contains(stdout, "daemon enable-linger") {
		t.Fatalf("runDaemon(help) output = %q / %q", stdout, stderr)
	}

	_, stderr = withCapturedOutput(t, func() {
		if got := runDaemon([]string{"bad"}); got != 1 {
			t.Fatalf("runDaemon(bad) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "unknown daemon command") {
		t.Fatalf("stderr = %q, want unknown daemon command", stderr)
	}

	if err := requireInstalled(&fakeManager{statusErr: errors.New("status failed")}); err == nil || err.Error() != "status failed" {
		t.Fatalf("requireInstalled(status err) = %v", err)
	}
	if err := requireInstalled(&fakeManager{statusResp: &daemon.Status{Installed: false}}); err == nil {
		t.Fatal("expected requireInstalled to fail when service is not installed")
	}
	if err := requireInstalled(&fakeManager{statusResp: &daemon.Status{Installed: true}}); err != nil {
		t.Fatalf("requireInstalled(installed) error = %v", err)
	}
}

func TestDaemonInstallAndLifecycleCommands(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	loadConfig = func(string) (*config.Config, error) { return minimalConfig(), nil }
	resolveDaemonConfig = func(cfg *daemon.Config) error {
		cfg.BinaryPath = "/bin/feidex"
		cfg.ConfigPath = filepath.Clean(cfg.ConfigPath)
		cfg.WorkDir = filepath.Dir(cfg.ConfigPath)
		return nil
	}

	mgr := &fakeManager{
		statusResp: &daemon.Status{Installed: false, Platform: "linux", UnitPath: "/tmp/feidex.service"},
		platform:   "linux",
	}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonInstall([]string{"--config", "config.toml"}); got != 0 {
		t.Fatalf("daemonInstall(success) = %d, want 0", got)
	}
	if len(mgr.calls) < 2 || mgr.calls[0] != "status" || mgr.calls[1] != "install" {
		t.Fatalf("daemonInstall calls = %+v, want status then install", mgr.calls)
	}

	enableLingerUser = func() error { return errors.New("linger failed") }
	if got := daemonInstall([]string{"--enable-linger"}); got != 1 {
		t.Fatalf("daemonInstall(linger error) = %d, want 1", got)
	}

	enableLingerUser = func() error { return nil }
	mgr = &fakeManager{statusResp: &daemon.Status{Installed: true}}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonInstall(nil); got != 1 {
		t.Fatalf("daemonInstall(already installed) = %d, want 1", got)
	}

	mgr = &fakeManager{statusResp: &daemon.Status{Installed: false}, installErr: errors.New("install failed")}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonInstall(nil); got != 1 {
		t.Fatalf("daemonInstall(install error) = %d, want 1", got)
	}

	enableLingerUser = func() error { return nil }
	if got := daemonEnableLinger(); got != 0 {
		t.Fatalf("daemonEnableLinger(success) = %d, want 0", got)
	}
	enableLingerUser = func() error { return errors.New("bad linger") }
	if got := daemonEnableLinger(); got != 1 {
		t.Fatalf("daemonEnableLinger(error) = %d, want 1", got)
	}

	mgr = &fakeManager{statusResp: &daemon.Status{Installed: true}}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonStart(); got != 0 {
		t.Fatalf("daemonStart(success) = %d, want 0", got)
	}
	if got := daemonStop(); got != 0 {
		t.Fatalf("daemonStop(success) = %d, want 0", got)
	}
	if got := daemonRestart(); got != 0 {
		t.Fatalf("daemonRestart(success) = %d, want 0", got)
	}

	mgr = &fakeManager{statusResp: &daemon.Status{Installed: false, Platform: "linux", UnitPath: "/tmp/feidex.service"}}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonStatus(); got != 0 {
		t.Fatalf("daemonStatus(not installed) = %d, want 0", got)
	}

	mgr = &fakeManager{statusResp: &daemon.Status{Installed: true, Running: true, Platform: "linux", UnitPath: "/tmp/feidex.service", PID: 99}}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonStatus(); got != 0 {
		t.Fatalf("daemonStatus(running) = %d, want 0", got)
	}

	mgr = &fakeManager{}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonUninstall(); got != 0 {
		t.Fatalf("daemonUninstall(success) = %d, want 0", got)
	}

	mgr = &fakeManager{removeErr: errors.New("remove failed")}
	newDaemonManager = func() (daemon.Manager, error) { return mgr, nil }
	if got := daemonUninstall(); got != 1 {
		t.Fatalf("daemonUninstall(error) = %d, want 1", got)
	}
}

func TestRunServeStopAllowsCanceledError(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	loadConfig = func(string) (*config.Config, error) { return minimalConfig(), nil }
	newApp = func(*config.Config, string) (appService, error) {
		return &fakeService{stopErr: context.Canceled}, nil
	}
	notifyCtx = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, func() {}
	}

	if got := runServe(nil); got != 0 {
		t.Fatalf("runServe(stop canceled) = %d, want 0", got)
	}
}

func TestSignalNotifyContextHelper(t *testing.T) {
	ctx, cancel := signalNotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected canceled context")
	}
}
