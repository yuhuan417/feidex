//go:build !linux && !darwin

package daemon

import (
	"fmt"
	"runtime"
)

func newPlatformManager(serviceName string) (Manager, error) {
	return nil, fmt.Errorf("systemd daemon management is not supported on %s", runtime.GOOS)
}

func EnableLingerCurrentUser() error {
	return fmt.Errorf("linger setup is not supported on %s", runtime.GOOS)
}
