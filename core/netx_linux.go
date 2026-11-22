//go:build linux

package openp2p

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func netxPlatformSupported() error {
	return nil
}

// netxDetectDefaultInterface returns the interface of the IPv4 default route
// from /proc/net/route, skipping tunnel and loopback interfaces.
func netxDetectDefaultInterface() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", fmt.Errorf("open /proc/net/route: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	first := true
	var skipped []string
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 {
			continue
		}
		iface, dst, mask := fields[0], fields[1], fields[7]
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&unix.RTF_UP == 0 {
			continue
		}
		if dst != "00000000" || mask != "00000000" {
			continue
		}
		if netxSkipInterface(iface) {
			skipped = append(skipped, iface)
			continue
		}
		return iface, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read /proc/net/route: %w", err)
	}
	if len(skipped) > 0 {
		return "", fmt.Errorf("no default route on a physical interface (skipped %s)", strings.Join(skipped, ", "))
	}
	return "", fmt.Errorf("no IPv4 default route found in /proc/net/route")
}

// netxBindSocket binds a socket to the configured interface. SO_BINDTODEVICE
// usually requires root privileges.
func netxBindSocket(fd uintptr) error {
	iface, _ := netxBindTarget()
	if iface == "" {
		return nil
	}
	if err := unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface); err != nil {
		return fmt.Errorf("set SO_BINDTODEVICE to %s: %w", iface, err)
	}
	return nil
}

// netxSetReuse enables address and port reuse before bind.
func netxSetReuse(fd uintptr) error {
	if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		return fmt.Errorf("set SO_REUSEADDR: %w", err)
	}
	if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		return fmt.Errorf("set SO_REUSEPORT: %w", err)
	}
	return nil
}
