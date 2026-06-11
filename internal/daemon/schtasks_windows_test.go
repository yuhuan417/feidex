//go:build windows

package daemon

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestVBSString(t *testing.T) {
	if got, want := vbsString(`a "b" c`), `"a ""b"" c"`; got != want {
		t.Fatalf("vbsString = %q, want %q", got, want)
	}
}

func TestPowershellSingleQuote(t *testing.T) {
	if got, want := powershellSingleQuote("o'brien"), "'o''brien'"; got != want {
		t.Fatalf("powershellSingleQuote = %q, want %q", got, want)
	}
}

func TestEncodeUTF16LE(t *testing.T) {
	b := encodeUTF16LE("AB")
	want := []byte{0xFF, 0xFE, 'A', 0x00, 'B', 0x00}
	if len(b) != len(want) {
		t.Fatalf("encodeUTF16LE len = %d, want %d", len(b), len(want))
	}
	for i := range want {
		if b[i] != want[i] {
			t.Fatalf("encodeUTF16LE[%d] = %#x, want %#x", i, b[i], want[i])
		}
	}
	// Round-trips back to the original text (excluding BOM).
	units := make([]uint16, 0, (len(b)-2)/2)
	for i := 2; i+1 < len(b); i += 2 {
		units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if got := string(utf16.Decode(units)); got != "AB" {
		t.Fatalf("round-trip = %q, want %q", got, "AB")
	}
}

func TestBuildLauncherVBSHidesWindowAndPropagatesExit(t *testing.T) {
	mgr := &schtasksManager{serviceName: DefaultServiceName}
	vbs := mgr.buildLauncherVBS(Config{
		BinaryPath: `C:\Program Files\feidex\feidex.exe`,
		ConfigPath: `C:\Users\tester\config.toml`,
	})
	for _, want := range []string{
		"WScript.Quit sh.Run(", // exit code propagates -> restart-on-failure
		", 0, True)",           // window style 0 = hidden, wait for the daemon
		`""C:\Program Files\feidex\feidex.exe"" serve --config`,
		"2>&1",
	} {
		if !strings.Contains(vbs, want) {
			t.Fatalf("launcher missing %q:\n%s", want, vbs)
		}
	}
}

func TestBuildTaskXMLContent(t *testing.T) {
	mgr := &schtasksManager{serviceName: DefaultServiceName}
	xml, err := mgr.buildTaskXML()
	if err != nil {
		t.Fatalf("buildTaskXML error = %v", err)
	}
	for _, want := range []string{
		`<Task version="1.2"`,
		"<LogonTrigger>",
		"<LogonType>InteractiveToken</LogonType>", // no stored password, runs in user session
		"<RunLevel>LeastPrivilege</RunLevel>",     // no admin
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<RestartOnFailure>",
		"<Command>wscript.exe</Command>", // windowless launcher host
		"-launch.vbs",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("task xml missing %q:\n%s", want, xml)
		}
	}
}

func TestNewPlatformManagerWindows(t *testing.T) {
	mgr, err := newPlatformManager(DefaultServiceName)
	if err != nil {
		t.Fatalf("newPlatformManager error = %v", err)
	}
	if mgr.Platform() != "Task Scheduler (Windows)" {
		t.Fatalf("Platform = %q", mgr.Platform())
	}
	if mgr.LogFile() == "" {
		t.Fatal("LogFile should be non-empty on Windows")
	}
}

func TestEnableLingerNoopOnWindows(t *testing.T) {
	if err := EnableLingerCurrentUser(); err != nil {
		t.Fatalf("EnableLingerCurrentUser on windows = %v, want nil", err)
	}
}
