package upgradecmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"feidex/internal/config"
)

func TestRemoteUpgradeSummary(t *testing.T) {
	tests := []struct {
		name          string
		forceVersion  bool
		useDevRelease bool
		expect        string
	}{
		{"latest", false, false, "确认后会下载新版本、重启 daemon；如果启动失败会自动回退到旧版本。"},
		{"dev release", false, true, "确认后会下载 `dev-latest` 当前指向的开发版构建、重启 daemon；如果启动失败会自动回退到旧版本。"},
		{"forced version", true, false, "已跳过最新版本检查。确认后会下载指定版本、重启 daemon；如果启动失败会自动回退到旧版本。"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoteUpgradeSummary(tt.forceVersion, tt.useDevRelease)
			if got != tt.expect {
				t.Fatalf("RemoteUpgradeSummary(%v, %v) = %q, want %q", tt.forceVersion, tt.useDevRelease, got, tt.expect)
			}
		})
	}
}

func TestUpgradePanelButtons(t *testing.T) {
	t.Run("without confirm or back", func(t *testing.T) {
		buttons := UpgradePanelButtons("sess-1", nil, false)
		if len(buttons) != 2 {
			t.Fatalf("expected 2 buttons, got %d", len(buttons))
		}
		if buttons[0].Text != "开发版" {
			t.Fatalf("first button text = %q, want 开发版", buttons[0].Text)
		}
		if buttons[1].Text != "选择本地 Binary" {
			t.Fatalf("second button text = %q, want 选择本地 Binary", buttons[1].Text)
		}
	})
	t.Run("with confirm", func(t *testing.T) {
		buttons := UpgradePanelButtons("sess-1", map[string]any{"label": "升级到 v1.0", "request_id": "r-1"}, false)
		if len(buttons) != 3 {
			t.Fatalf("expected 3 buttons, got %d", len(buttons))
		}
		if buttons[0].Text != "升级到 v1.0" {
			t.Fatalf("confirm button text = %q", buttons[0].Text)
		}
		if buttons[0].Type != "primary" {
			t.Fatalf("confirm button type = %q, want primary", buttons[0].Type)
		}
	})
	t.Run("with back", func(t *testing.T) {
		buttons := UpgradePanelButtons("sess-1", nil, true)
		if len(buttons) != 3 {
			t.Fatalf("expected 3 buttons, got %d", len(buttons))
		}
		if buttons[2].Text != "返回上一级" {
			t.Fatalf("back button text = %q", buttons[2].Text)
		}
	})
	t.Run("confirm with empty label uses default", func(t *testing.T) {
		buttons := UpgradePanelButtons("sess-1", map[string]any{"request_id": "r-1"}, false)
		if buttons[0].Text != "确认升级" {
			t.Fatalf("default confirm label = %q, want 确认升级", buttons[0].Text)
		}
	})
}

func TestShortUpgradeCommit(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"short", "abc123", "abc123"},
		{"exact 12", "123456789012", "123456789012"},
		{"long truncates", "12345678901234567890", "123456789012"},
		{"trims whitespace", "  abc  ", "abc"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShortUpgradeCommit(tt.input)
			if got != tt.expect {
				t.Fatalf("ShortUpgradeCommit(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestFormatUpgradeReleasePublishedAt(t *testing.T) {
	t.Run("zero time", func(t *testing.T) {
		if got := FormatUpgradeReleasePublishedAt(time.Time{}); got != "" {
			t.Fatalf("expected empty for zero time, got %q", got)
		}
	})
	t.Run("formats time", func(t *testing.T) {
		ts := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
		got := FormatUpgradeReleasePublishedAt(ts)
		if got == "" {
			t.Fatal("expected non-empty formatted time")
		}
	})
}

func TestUpgradeStartedSummaryLine(t *testing.T) {
	t.Run("local source", func(t *testing.T) {
		payload := UpgradePendingPayload{
			SourcePath: "/tmp/my-binary",
			SourceName: "custom-binary",
		}
		got := UpgradeStartedSummaryLine(payload)
		if got != "本地制品: `custom-binary`" {
			t.Fatalf("unexpected: %q", got)
		}
	})
	t.Run("remote version", func(t *testing.T) {
		payload := UpgradePendingPayload{TargetVersion: "v1.2.3"}
		got := UpgradeStartedSummaryLine(payload)
		if got != "目标版本: `v1.2.3`" {
			t.Fatalf("unexpected: %q", got)
		}
	})
	t.Run("remote with different tag", func(t *testing.T) {
		payload := UpgradePendingPayload{
			TargetVersion: "v1.2.3",
			ReleaseTag:    "release-2025-03",
			SourceCommit:  "abc123def456",
		}
		got := UpgradeStartedSummaryLine(payload)
		if got == "" {
			t.Fatal("expected non-empty")
		}
	})
}

func TestUpgradeLocalConfirmLines(t *testing.T) {
	svc := UpgradeService{
		deps: UpgradeServiceDeps{
			CurrentVersion: func() string { return "v0.1.0" },
			CurrentGOARCH:  func() string { return "amd64" },
		},
	}

	lines := svc.UpgradeLocalConfirmLines("/usr/local/bin/feidex")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "当前版本: `v0.1.0`" {
		t.Fatalf("line 0 = %q", lines[0])
	}
	if lines[1] != "目标架构: `amd64`" {
		t.Fatalf("line 1 = %q", lines[1])
	}
	if lines[2] != "二进制: `/usr/local/bin/feidex`" {
		t.Fatalf("line 2 = %q", lines[2])
	}
}

func TestResolveUpgradeLocalSourcePath(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "mybin")
	if err := os.WriteFile(binPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}
	ws := &config.Workspace{ID: "test", Cwd: dir}

	t.Run("valid file", func(t *testing.T) {
		got, err := ResolveUpgradeLocalSourcePath(ws, "mybin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != binPath {
			t.Fatalf("got %q, want %q", got, binPath)
		}
	})
	t.Run("directory rejected", func(t *testing.T) {
		_, err := ResolveUpgradeLocalSourcePath(ws, ".")
		if err == nil {
			t.Fatal("expected error for directory")
		}
	})
	t.Run("nonexistent rejected", func(t *testing.T) {
		_, err := ResolveUpgradeLocalSourcePath(ws, "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
	t.Run("nil workspace rejected", func(t *testing.T) {
		_, err := ResolveUpgradeLocalSourcePath(nil, "mybin")
		if err == nil {
			t.Fatal("expected error for nil workspace")
		}
	})
}

func TestNewUpgradeLocalPickerPayload(t *testing.T) {
	dir := t.TempDir()
	ws := &config.Workspace{ID: "test", Cwd: dir}

	payload, err := NewUpgradeLocalPickerPayload(ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload.RootPath != dir {
		t.Fatalf("root path = %q, want %q", payload.RootPath, dir)
	}
	if payload.CurrentPath != dir {
		t.Fatalf("current path = %q, want %q", payload.CurrentPath, dir)
	}
}
