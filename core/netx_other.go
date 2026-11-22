//go:build !darwin && !linux

package openp2p

import (
	"fmt"
	"runtime"
)

func netxPlatformSupported() error {
	return fmt.Errorf("netx: interface binding is not supported on %s", runtime.GOOS)
}

func netxDetectDefaultInterface() (string, error) {
	return "", fmt.Errorf("netx: automatic interface detection is not supported on %s", runtime.GOOS)
}

func netxBindSocket(fd uintptr) error {
	return fmt.Errorf("netx: interface binding is not supported on %s", runtime.GOOS)
}

func netxSetReuse(fd uintptr) error {
	return fmt.Errorf("netx: socket reuse is not supported on %s", runtime.GOOS)
}
