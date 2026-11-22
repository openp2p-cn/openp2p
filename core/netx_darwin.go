//go:build darwin

package openp2p

import (
	"fmt"
	"strings"
	"syscall"

	"golang.org/x/net/route"
)

func netxPlatformSupported() error {
	return nil
}

// netxDetectDefaultInterface returns the interface of the IPv4 default route,
// skipping tunnel and loopback interfaces.
func netxDetectDefaultInterface() (string, error) {
	rib, err := route.FetchRIB(syscall.AF_INET, route.RIBTypeRoute, 0)
	if err != nil {
		return "", fmt.Errorf("read IPv4 routing table: %w", err)
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return "", fmt.Errorf("parse IPv4 routing table: %w", err)
	}
	names, err := netxInterfaceNames()
	if err != nil {
		return "", err
	}
	var skipped []string
	for _, m := range msgs {
		rm, ok := m.(*route.RouteMessage)
		if !ok || rm.Flags&syscall.RTF_UP == 0 || !netxRouteIsDefault(rm) {
			continue
		}
		name := netxRouteInterfaceName(rm, names)
		if name == "" {
			continue
		}
		if netxSkipInterface(name) {
			skipped = append(skipped, name)
			continue
		}
		return name, nil
	}
	if len(skipped) > 0 {
		return "", fmt.Errorf("no default route on a physical interface (skipped %s)", strings.Join(skipped, ", "))
	}
	return "", fmt.Errorf("no IPv4 default route found")
}

// netxInterfaceNames maps interface indexes to names using the routing table.
func netxInterfaceNames() (map[int]string, error) {
	rib, err := route.FetchRIB(syscall.AF_UNSPEC, route.RIBTypeInterface, 0)
	if err != nil {
		return nil, fmt.Errorf("read interface list: %w", err)
	}
	msgs, err := route.ParseRIB(route.RIBTypeInterface, rib)
	if err != nil {
		return nil, fmt.Errorf("parse interface list: %w", err)
	}
	names := make(map[int]string)
	for _, m := range msgs {
		if im, ok := m.(*route.InterfaceMessage); ok {
			names[im.Index] = im.Name
		}
	}
	return names, nil
}

// netxRouteIsDefault reports whether rm is a default route.
func netxRouteIsDefault(rm *route.RouteMessage) bool {
	if len(rm.Addrs) <= syscall.RTAX_NETMASK {
		return false
	}
	if !netxZeroRouteAddr(rm.Addrs[syscall.RTAX_DST]) {
		return false
	}
	return netxZeroRouteAddr(rm.Addrs[syscall.RTAX_NETMASK])
}

func netxZeroRouteAddr(addr route.Addr) bool {
	switch a := addr.(type) {
	case *route.Inet4Addr:
		return a.IP == ([4]byte{})
	case *route.Inet6Addr:
		return a.IP == ([16]byte{})
	case nil:
		return true
	}
	return false
}

// netxRouteInterfaceName resolves the interface for a route message.
func netxRouteInterfaceName(rm *route.RouteMessage, names map[int]string) string {
	if name, ok := names[rm.Index]; ok {
		return name
	}
	if len(rm.Addrs) > syscall.RTAX_IFP {
		if link, ok := rm.Addrs[syscall.RTAX_IFP].(*route.LinkAddr); ok {
			if name, ok := names[link.Index]; ok {
				return name
			}
			return link.Name
		}
	}
	return ""
}

// netxBindSocket binds a socket to the configured interface. The socket
// family is read from getsockname because the dialer network may be "tcp"
// or "udp" with the actual family chosen after name resolution.
func netxBindSocket(fd uintptr) error {
	iface, index := netxBindTarget()
	if iface == "" {
		return nil
	}
	if index <= 0 {
		return fmt.Errorf("interface %q has no index", iface)
	}
	sa, err := syscall.Getsockname(int(fd))
	if err != nil {
		return fmt.Errorf("getsockname: %w", err)
	}
	switch sa.(type) {
	case *syscall.SockaddrInet6:
		if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_BOUND_IF, index); err != nil {
			return fmt.Errorf("set IPV6_BOUND_IF to %s: %w", iface, err)
		}
	case *syscall.SockaddrInet4:
		if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BOUND_IF, index); err != nil {
			return fmt.Errorf("set IP_BOUND_IF to %s: %w", iface, err)
		}
	default:
		return fmt.Errorf("unsupported socket family %T", sa)
	}
	return nil
}

// netxSetReuse enables address and port reuse before bind.
func netxSetReuse(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return fmt.Errorf("set SO_REUSEADDR: %w", err)
	}
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
		return fmt.Errorf("set SO_REUSEPORT: %w", err)
	}
	return nil
}
