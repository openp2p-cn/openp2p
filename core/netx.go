package openp2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	reuse "github.com/openp2p-cn/go-reuseport"
)

// netx implements optional outbound socket binding to a network interface.
// When no interface is configured every helper falls back to the standard
// library behavior, so the default runtime path is unchanged.

var (
	netxMu    sync.RWMutex
	netxState netxConfig

	// netxAutoDetect is a variable so tests can simulate a missing default route.
	netxAutoDetect = netxDetectDefaultInterface
)

type netxConfig struct {
	iface    string
	index    int
	resolver *net.Resolver
}

// netxDefaultDNS is used when an interface is bound without explicit servers.
var netxDefaultDNS = []string{"119.29.29.29:53", "223.5.5.5:53"}

// netxConfigure applies the interface binding configuration. An empty iface
// disables binding; "auto" selects the interface of the IPv4 default route.
func netxConfigure(iface string, dns []string) error {
	iface = strings.TrimSpace(iface)
	if iface == "" {
		netxMu.Lock()
		netxState = netxConfig{}
		netxMu.Unlock()
		return nil
	}
	if err := netxPlatformSupported(); err != nil {
		return err
	}
	name := iface
	if iface == "auto" {
		var err error
		name, err = netxAutoDetect()
		if err != nil {
			return fmt.Errorf("netx: auto-detect interface: %w", err)
		}
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return fmt.Errorf("netx: interface %q: %w", name, err)
	}
	servers, err := netxNormalizeDNS(dns)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		servers = append([]string(nil), netxDefaultDNS...)
		netxLogf("no DNS servers configured, using %s", strings.Join(servers, ", "))
	}
	cfg := netxConfig{
		iface: ifi.Name,
		index: ifi.Index,
		resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return netxDialDNSServers(ctx, network, servers)
			},
		},
	}
	netxMu.Lock()
	netxState = cfg
	netxMu.Unlock()
	netxLogf("binding outbound sockets to %s (index %d), DNS %s", cfg.iface, cfg.index, strings.Join(servers, ", "))
	return nil
}

func netxLogf(format string, args ...interface{}) {
	if gLog != nil {
		gLog.i("netx: "+format, args...)
	}
}

// netxNormalizeDNS accepts bare IPs or host:port entries and returns
// host:port entries. A missing port defaults to 53.
func netxNormalizeDNS(dns []string) ([]string, error) {
	servers := make([]string, 0, len(dns))
	for _, item := range dns {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		addr, err := netxNormalizeDNSServer(item)
		if err != nil {
			return nil, err
		}
		servers = append(servers, addr)
	}
	return servers, nil
}

// DNS servers must be IP addresses: resolving a server hostname with the
// pinned resolver would recursively need the resolver itself.
func netxNormalizeDNSServer(server string) (string, error) {
	if host, port, err := net.SplitHostPort(server); err == nil {
		ip := net.ParseIP(host)
		if ip == nil || !netxValidPort(port) {
			return "", fmt.Errorf("netx: invalid DNS server %q, want IP:port", server)
		}
		return net.JoinHostPort(ip.String(), port), nil
	}
	host := strings.Trim(server, "[]")
	if ip := net.ParseIP(host); ip != nil {
		return net.JoinHostPort(ip.String(), "53"), nil
	}
	return "", fmt.Errorf("netx: invalid DNS server %q, want an IP address or IP:port", server)
}

func netxValidPort(port string) bool {
	p, err := strconv.Atoi(port)
	return err == nil && p > 0 && p <= 65535
}

// netxSkipInterface reports whether name is a virtual or tunnel interface
// that "auto" should not select. Explicit interface names are always honored.
func netxSkipInterface(name string) bool {
	for _, prefix := range []string{
		"utun", "tun", "tap", "lo", "Meta", "clash", "mihomo", "wg",
		"tailscale", "sing", "bridge", "vmnet", "veth", "docker", "ppp",
		"ipsec", "gif", "stf", "awdl", "llw", "anpi",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// netxEnabled reports whether outbound sockets are bound to an interface.
func netxEnabled() bool {
	return netxInterfaceName() != ""
}

// netxInterfaceName returns the configured interface name, if any.
func netxInterfaceName() string {
	netxMu.RLock()
	defer netxMu.RUnlock()
	return netxState.iface
}

// netxBindTarget returns the configured interface name and index.
func netxBindTarget() (string, int) {
	netxMu.RLock()
	defer netxMu.RUnlock()
	return netxState.iface, netxState.index
}

// netxResolver returns the pinned DNS resolver, or nil when binding is off.
func netxResolver() *net.Resolver {
	netxMu.RLock()
	defer netxMu.RUnlock()
	return netxState.resolver
}

// netxControl binds a new socket to the configured interface.
func netxControl(network, address string, c syscall.RawConn) error {
	if !netxEnabled() {
		return nil
	}
	return netxBindRawConn(c)
}

// netxReuseControl sets SO_REUSEADDR/SO_REUSEPORT and binds the socket.
// It is used by TCP hole punching, where the local port must be reused.
func netxReuseControl(network, address string, c syscall.RawConn) error {
	if !netxEnabled() {
		return nil
	}
	iface, _ := netxBindTarget()
	var sockErr error
	if err := c.Control(func(fd uintptr) {
		if err := netxSetReuse(fd); err != nil {
			sockErr = err
			return
		}
		sockErr = netxBindSocket(fd)
	}); err != nil {
		return fmt.Errorf("netx: control socket for %q: %w", iface, err)
	}
	if sockErr != nil {
		return fmt.Errorf("netx: configure socket for %q: %w", iface, sockErr)
	}
	return nil
}

// netxBindRawConn binds an existing socket to the configured interface.
func netxBindRawConn(c syscall.RawConn) error {
	iface, _ := netxBindTarget()
	var sockErr error
	if err := c.Control(func(fd uintptr) {
		sockErr = netxBindSocket(fd)
	}); err != nil {
		return fmt.Errorf("netx: control socket for %q: %w", iface, err)
	}
	if sockErr != nil {
		return fmt.Errorf("netx: bind socket to %q: %w", iface, sockErr)
	}
	return nil
}

// netxDialer returns a dialer using the pinned resolver and interface binding
// when binding is enabled. When it is disabled the dialer is a plain one.
func netxDialer(timeout time.Duration) *net.Dialer {
	dialer := &net.Dialer{Timeout: timeout}
	if netxEnabled() {
		dialer.Control = netxControl
		dialer.Resolver = netxResolver()
	}
	return dialer
}

// netxDialContext dials with the configured interface binding.
func netxDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return netxDialer(0).DialContext(ctx, network, address)
}

// netxDialTimeout dials with the configured interface binding and a timeout.
func netxDialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	return netxDialer(timeout).DialContext(context.Background(), network, address)
}

// netxDialTCPString dials a TCP address with an optional fixed local address.
// SO_REUSEADDR/SO_REUSEPORT are set before bind for TCP hole punching.
func netxDialTCPString(network, laddr, raddr string, timeout time.Duration) (net.Conn, error) {
	if !netxEnabled() {
		// Keep the stock behavior: a fixed local port still needs reuse so a
		// second socket can bind the same port, while a plain dial does not.
		if strings.TrimSpace(laddr) == "" {
			return net.DialTimeout(network, raddr, timeout)
		}
		return reuse.DialTimeout(network, laddr, raddr, timeout)
	}
	var local *net.TCPAddr
	if strings.TrimSpace(laddr) != "" {
		addr, err := net.ResolveTCPAddr(network, laddr)
		if err != nil {
			return nil, fmt.Errorf("netx: resolve local address %q: %w", laddr, err)
		}
		local = addr
	}
	dialer := netxDialer(timeout)
	dialer.Control = netxReuseControl
	if local != nil {
		dialer.LocalAddr = local
	}
	// Hand the remote address to the dialer as-is so it is resolved with the
	// pinned resolver instead of the system resolver.
	return dialer.DialContext(context.Background(), network, raddr)
}

// netxListenPacket listens on a packet socket bound to the configured interface.
func netxListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	if !netxEnabled() {
		return net.ListenPacket(network, address)
	}
	lc := net.ListenConfig{Control: netxControl}
	return lc.ListenPacket(ctx, network, address)
}

// netxListenUDP listens on a UDP socket bound to the configured interface.
func netxListenUDP(network string, laddr *net.UDPAddr) (*net.UDPConn, error) {
	if !netxEnabled() {
		return net.ListenUDP(network, laddr)
	}
	lc := net.ListenConfig{Control: netxControl}
	address := ":0"
	if laddr != nil {
		address = laddr.String()
	}
	conn, err := lc.ListenPacket(context.Background(), network, address)
	if err != nil {
		return nil, err
	}
	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("netx: listen udp %s: unexpected connection type %T", network, conn)
	}
	return udpConn, nil
}

// netxListenTCP listens on a TCP socket bound to the configured interface.
func netxListenTCP(network string, laddr *net.TCPAddr) (*net.TCPListener, error) {
	if !netxEnabled() {
		return net.ListenTCP(network, laddr)
	}
	lc := net.ListenConfig{Control: netxControl}
	address := ":0"
	if laddr != nil {
		address = laddr.String()
	}
	listener, err := lc.Listen(context.Background(), network, address)
	if err != nil {
		return nil, err
	}
	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		listener.Close()
		return nil, fmt.Errorf("netx: listen tcp %s: unexpected listener type %T", network, listener)
	}
	return tcpListener, nil
}

// netxBindConn binds an accepted connection to the configured interface.
func netxBindConn(c net.Conn) error {
	if !netxEnabled() || c == nil {
		return nil
	}
	sysConn, ok := c.(syscall.Conn)
	if !ok {
		return fmt.Errorf("netx: connection type %T does not expose a raw socket", c)
	}
	raw, err := sysConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("netx: raw connection: %w", err)
	}
	return netxBindRawConn(raw)
}

// netxTransport returns an HTTP transport bound to the configured interface.
func netxTransport() *http.Transport {
	return &http.Transport{DialContext: netxDialContext}
}

// netxResolve resolves host using the pinned DNS servers when binding is on.
func netxResolve(ctx context.Context, host string) ([]string, error) {
	if r := netxResolver(); r != nil {
		return r.LookupHost(ctx, host)
	}
	return net.DefaultResolver.LookupHost(ctx, host)
}

// netxDialDNSServers tries the pinned DNS servers in order.
func netxDialDNSServers(ctx context.Context, network string, servers []string) (net.Conn, error) {
	var lastErr error
	for _, server := range servers {
		conn, err := netxDialContext(ctx, network, server)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("netx: no DNS servers configured")
	}
	return nil, lastErr
}

// netxRejectFakeIP rejects addresses from the 198.18.0.0/15 fake-IP range.
func netxRejectFakeIP(ips []string) error {
	for _, ipStr := range ips {
		ip := net.ParseIP(strings.TrimSpace(ipStr))
		if ip == nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 != nil && ip4[0] == 198 && (ip4[1]&0xfe) == 18 {
			return fmt.Errorf("netx: address %s is in the fake-IP range 198.18.0.0/15", ipStr)
		}
	}
	return nil
}
