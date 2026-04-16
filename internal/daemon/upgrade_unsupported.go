//go:build !linux

package daemon

import (
	"context"
	"fmt"
)

type UpgradeSpec struct {
	ServiceName    string
	Version        string
	BinaryPath     string
	DownloadURL    string
	SourcePath     string
	ExpectedSHA256 string
}

func StartBackgroundUpgrade(spec UpgradeSpec) (string, error) {
	return "", fmt.Errorf("daemon self-upgrade is not supported on this platform")
}

func RunUpgradeRunner(ctx context.Context, spec UpgradeSpec) error {
	return fmt.Errorf("daemon self-upgrade is not supported on this platform")
}
