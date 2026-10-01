package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withStubbedRunner(t *testing.T, runner func(context.Context, string, ...string) (string, string, error)) {
	t.Helper()
	prev := commandRunner
	commandRunner = runner
	t.Cleanup(func() { commandRunner = prev })
}

func withStubbedLatestVersion(t *testing.T, lookup func(context.Context, string, string) (string, error)) {
	t.Helper()
	prev := latestVersionLookup
	latestVersionLookup = lookup
	t.Cleanup(func() { latestVersionLookup = prev })
}

func withStubbedCodexUserAgent(t *testing.T, lookup func(context.Context, string) (string, error)) {
	t.Helper()
	prev := codexUserAgentLookup
	codexUserAgentLookup = lookup
	t.Cleanup(func() { codexUserAgentLookup = prev })
}

// writeFakeCommand puts an executable stub on PATH so exec.LookPath succeeds.
func writeFakeCommand(t *testing.T, name string) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	commandPath := filepath.Join(binDir, name)
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath); err != nil {
		t.Fatalf("Setenv(PATH) error = %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", originalPath) })
	return commandPath
}

func TestManagerProbeDetectsSupportedSelfUpdateCommand(t *testing.T) {
	cases := []struct {
		name            string
		spec            Spec
		versionOutput   string
		expectedVersion string
	}{
		{
			name:            "claude",
			spec:            Claude(),
			versionOutput:   "2.1.138 (Claude Code)",
			expectedVersion: "2.1.138",
		},
		{
			name:            "codex",
			spec:            Codex(),
			versionOutput:   "WARNING: ignored\ncodex-cli 0.132.0",
			expectedVersion: "0.132.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandPath := writeFakeCommand(t, tc.name)
			withStubbedRunner(t, func(_ context.Context, name string, args ...string) (string, string, error) {
				switch {
				case name == tc.name && len(args) == 1 && args[0] == "--version":
					return tc.versionOutput, "", nil
				case name == tc.name && len(args) == 2 && args[0] == "update" && args[1] == "--help":
					return "update help", "", nil
				default:
					return "", "", errors.New("unexpected command")
				}
			})

			probe, err := New(tc.spec, tc.name).Probe(context.Background())
			if err != nil {
				t.Fatalf("Probe() error = %v", err)
			}
			if !probe.Supported {
				t.Fatalf("Probe().Supported = false, reason=%q", probe.Reason)
			}
			if probe.CurrentVersion != tc.expectedVersion {
				t.Fatalf("Probe().CurrentVersion = %q, want %q", probe.CurrentVersion, tc.expectedVersion)
			}
			if probe.UpdateCommand != "update" {
				t.Fatalf("Probe().UpdateCommand = %q, want update", probe.UpdateCommand)
			}
			if probe.CommandPath != commandPath {
				t.Fatalf("Probe().CommandPath = %q, want %q", probe.CommandPath, commandPath)
			}
		})
	}
}

func TestManagerProbeRejectsMissingCommand(t *testing.T) {
	for _, spec := range []Spec{Claude(), Codex()} {
		t.Run(spec.CommandName, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "missing-"+spec.CommandName)
			probe, err := New(spec, missing).Probe(context.Background())
			if err != nil {
				t.Fatalf("Probe(missing) error = %v", err)
			}
			if probe.Supported {
				t.Fatal("Probe(missing) should be unsupported")
			}
			if probe.Reason == "" {
				t.Fatal("Probe(missing) should include reason")
			}
		})
	}
}

func TestManagerLatestVersionAndInstallVersionUseSelfUpdate(t *testing.T) {
	cases := []struct {
		name               string
		spec               Spec
		expectedPackage    string
		expectedUserAgent  string
		expectedLatest     string
		latestVersionReply string
	}{
		{
			name:              "claude",
			spec:              Claude(),
			expectedPackage:   "@anthropic-ai/claude-code",
			expectedUserAgent: "claude-cli/2.1.138 (external, cli)",
		},
		{
			name:              "codex",
			spec:              Codex(),
			expectedPackage:   "@openai/codex",
			expectedUserAgent: "codex_cli_rs/0.132.0 (Debian 13.0.0; x86_64) dumb (codex_cli_rs; 0.132.0)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			versionOutput := "2.1.138 (Claude Code)"
			if tc.spec.CommandName == "codex" {
				versionOutput = "codex-cli 0.132.0"
			}

			var updates [][]string
			withStubbedRunner(t, func(_ context.Context, name string, args ...string) (string, string, error) {
				switch {
				case name == tc.spec.CommandName && len(args) == 1 && args[0] == "--version":
					return versionOutput, "", nil
				case name == tc.spec.CommandName && len(args) == 2 && args[0] == "update" && args[1] == "--help":
					return "update help", "", nil
				case name == tc.spec.CommandName && len(args) == 1 && args[0] == "update":
					updates = append(updates, append([]string(nil), args...))
					return "updated", "", nil
				default:
					return "", "", errors.New("unexpected command")
				}
			})
			withStubbedCodexUserAgent(t, func(_ context.Context, command string) (string, error) {
				if command != "codex" {
					t.Fatalf("User-Agent command = %q, want codex", command)
				}
				return tc.expectedUserAgent, nil
			})
			withStubbedLatestVersion(t, func(_ context.Context, packageName, userAgent string) (string, error) {
				if packageName != tc.expectedPackage {
					t.Fatalf("latest package = %q, want %q", packageName, tc.expectedPackage)
				}
				if userAgent != tc.expectedUserAgent {
					t.Fatalf("latest User-Agent = %q, want %q", userAgent, tc.expectedUserAgent)
				}
				return "9.9.9", nil
			})

			manager := New(tc.spec, tc.spec.CommandName)
			version, err := manager.LatestVersion(context.Background())
			if err != nil {
				t.Fatalf("LatestVersion() error = %v", err)
			}
			if version != "9.9.9" {
				t.Fatalf("LatestVersion() = %q, want 9.9.9", version)
			}
			if err := manager.InstallVersion(context.Background(), selfUpdateTargetLatest); err != nil {
				t.Fatalf("InstallVersion() error = %v", err)
			}
			if len(updates) != 1 || updates[0][0] != "update" {
				t.Fatalf("InstallVersion() updates = %#v", updates)
			}
			if err := manager.InstallVersion(context.Background(), "1.2.3"); err == nil {
				t.Fatal("InstallVersion(specific version) should fail")
			}
		})
	}
}

func TestCodexUserAgentLookupUsesInitializeResponse(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "rpc.log")
	scriptPath := filepath.Join(tempDir, "codex-rpc.sh")
	const wantUserAgent = "codex_cli_rs/0.132.0 (Debian 13.0.0; x86_64) dumb (codex_cli_rs; 0.132.0)"
	script := fmt.Sprintf(`#!/bin/sh
logfile=%q
while IFS= read -r line; do
  printf '%%s\n' "$line" >> "$logfile"
  case "$line" in
    *'"method":"initialize"'*)
      printf '%%s\n' '{"id":1,"result":{"userAgent":"%s","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
      ;;
    *'"method":"initialized"'*)
      exit 0
      ;;
  esac
done
`, logPath, wantUserAgent)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	got, err := codexUserAgentLookup(context.Background(), scriptPath)
	if err != nil {
		t.Fatalf("codexUserAgentLookup() error = %v", err)
	}
	if got != wantUserAgent {
		t.Fatalf("codexUserAgentLookup() = %q, want %q", got, wantUserAgent)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(logPath) error = %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, `"method":"initialize"`) {
		t.Fatalf("rpc log = %q, want initialize", logText)
	}
	if !strings.Contains(logText, `"name":"codex_cli_rs"`) {
		t.Fatalf("rpc log = %q, want standard Codex CLI clientInfo", logText)
	}
}
