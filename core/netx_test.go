package openp2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// netxTestLoopback returns the loopback interface name for the current OS.
func netxTestLoopback() string {
	switch runtime.GOOS {
	case "darwin":
		return "lo0"
	case "linux":
		return "lo"
	default:
		return ""
	}
}

// netxTestReset disables interface binding before and after a test.
func netxTestReset(t *testing.T) {
	t.Helper()
	if err := netxConfigure("", nil); err != nil {
		t.Fatalf("disable interface binding: %v", err)
	}
	t.Cleanup(func() {
		if err := netxConfigure("", nil); err != nil {
			t.Errorf("disable interface binding: %v", err)
		}
	})
}

func TestNetxLoopbackBindings(t *testing.T) {
	loopback := netxTestLoopback()
	if loopback == "" {
		t.Skipf("interface binding is not supported on %s", runtime.GOOS)
	}
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		t.Skip("SO_BINDTODEVICE requires root on Linux")
	}
	netxTestReset(t)
	if err := netxConfigure(loopback, nil); err != nil {
		t.Fatalf("configure %s: %v", loopback, err)
	}
	if !netxEnabled() {
		t.Fatal("expected binding to be enabled")
	}
	if name := netxInterfaceName(); name != loopback {
		t.Fatalf("interface name = %q, want %q", name, loopback)
	}
	if netxResolver() == nil {
		t.Fatal("expected a pinned resolver")
	}

	// TCP listener + dial + accepted connection binding.
	ln, err := netxListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()
	if err := ln.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set listener deadline: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			done <- err
			return
		}
		if err := netxBindConn(conn); err != nil {
			done <- fmt.Errorf("bind accepted connection: %w", err)
			return
		}
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			done <- err
			return
		}
		if string(buf) != "ping" {
			done <- fmt.Errorf("server read %q, want ping", buf)
			return
		}
		_, err = conn.Write([]byte("pong"))
		done <- err
	}()
	conn, err := netxDialContext(context.Background(), "tcp4", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set connection deadline: %v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write tcp: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read tcp: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("client read %q, want pong", buf)
	}
	conn.Close()
	if err := <-done; err != nil {
		t.Fatalf("tcp server: %v", err)
	}

	// IPv6 loopback exercises the IPV6_BOUND_IF path when available.
	if probe, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		probe.Close()
		ln6, err := netxListenTCP("tcp6", &net.TCPAddr{IP: net.IPv6loopback})
		if err != nil {
			t.Fatalf("listen tcp6: %v", err)
		}
		defer ln6.Close()
		if err := ln6.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("set tcp6 listener deadline: %v", err)
		}
		done6 := make(chan error, 1)
		go func() {
			conn, err := ln6.Accept()
			if err == nil {
				conn.Close()
			}
			done6 <- err
		}()
		conn6, err := netxDialContext(context.Background(), "tcp6", ln6.Addr().String())
		if err != nil {
			t.Fatalf("dial tcp6: %v", err)
		}
		conn6.Close()
		if err := <-done6; err != nil {
			t.Fatalf("tcp6 accept: %v", err)
		}
	} else {
		t.Logf("IPv6 loopback unavailable: %v", err)
	}

	// UDP listener + dial.
	pc, err := netxListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()
	if err := pc.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set udp deadline: %v", err)
	}
	uc, err := netxDialContext(context.Background(), "udp4", pc.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer uc.Close()
	if _, err := uc.Write([]byte("ping")); err != nil {
		t.Fatalf("write udp: %v", err)
	}
	buf = make([]byte, 4)
	n, from, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read udp: %v", err)
	}
	if string(buf[:n]) != "ping" {
		t.Fatalf("udp server read %q, want ping", buf[:n])
	}
	if _, err := pc.WriteTo([]byte("pong"), from); err != nil {
		t.Fatalf("write udp reply: %v", err)
	}
	if _, err := uc.Read(buf); err != nil {
		t.Fatalf("read udp reply: %v", err)
	}

	// Packet listener used by QUIC-style transports.
	pconn, err := netxListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen packet: %v", err)
	}
	if pconn.LocalAddr() == nil {
		t.Fatal("packet listener has no local address")
	}
	pconn.Close()

	// Callers also listen on wildcard addresses with a generic network.
	wildUDP, err := netxListenUDP("udp", nil)
	if err != nil {
		t.Fatalf("listen wildcard udp: %v", err)
	}
	wildUDP.Close()
	wildTCP, err := netxListenTCP("tcp", nil)
	if err != nil {
		t.Fatalf("listen wildcard tcp: %v", err)
	}
	wildTCP.Close()
}

func TestNetxDialTCPStringKeepsLocalPort(t *testing.T) {
	loopback := netxTestLoopback()
	if loopback == "" {
		t.Skipf("interface binding is not supported on %s", runtime.GOOS)
	}
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		t.Skip("SO_BINDTODEVICE requires root on Linux")
	}
	netxTestReset(t)
	if err := netxConfigure(loopback, nil); err != nil {
		t.Fatalf("configure %s: %v", loopback, err)
	}

	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer server.Close()
	go func() {
		conn, err := server.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	local := probe.Addr().String()
	wantPort := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	conn, err := netxDialTCPString("tcp4", local, server.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial with fixed local address %s: %v", local, err)
	}
	defer conn.Close()
	got, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("local address type %T, want *net.TCPAddr", conn.LocalAddr())
	}
	if got.Port != wantPort {
		t.Fatalf("local port = %d, want %d", got.Port, wantPort)
	}
}

func TestNetxDisabledDialTCPStringEmptyLocalAddr(t *testing.T) {
	netxTestReset(t)
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer server.Close()
	go func() {
		conn, err := server.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	// The disabled path with no fixed local address must use a plain dial,
	// not the reuse-port control hook used by TCP hole punching.
	conn, err := netxDialTCPString("tcp4", "", server.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("disabled dial with empty local address: %v", err)
	}
	conn.Close()
	if netxEnabled() {
		t.Fatal("binding unexpectedly enabled")
	}
}

func TestNetxRejectFakeIP(t *testing.T) {
	if err := netxRejectFakeIP(nil); err != nil {
		t.Fatalf("empty addresses rejected: %v", err)
	}
	if err := netxRejectFakeIP([]string{"1.2.3.4", "2001:db8::1", "not-an-ip"}); err != nil {
		t.Fatalf("public addresses rejected: %v", err)
	}
	if err := netxRejectFakeIP([]string{"198.20.0.1"}); err != nil {
		t.Fatalf("198.20.0.1 rejected: %v", err)
	}
	for _, ip := range []string{"198.18.0.1", "198.18.255.255", "198.19.0.1", "198.19.255.254", "::ffff:198.18.1.1"} {
		if err := netxRejectFakeIP([]string{ip}); err == nil {
			t.Errorf("fake IP %s was not rejected", ip)
		}
	}
}

func TestNetxAutoDetect(t *testing.T) {
	netxTestReset(t)
	if err := netxConfigure("auto", nil); err != nil {
		t.Skipf("auto-detection unavailable: %v", err)
	}
	if !netxEnabled() {
		t.Fatal("binding disabled after auto-detection")
	}
	name := netxInterfaceName()
	if name == "" {
		t.Fatal("auto-detection selected an empty interface name")
	}
	t.Logf("auto-detected interface %s", name)
}

func TestNetxInvalidInterface(t *testing.T) {
	netxTestReset(t)
	if err := netxConfigure("netx-no-such-interface0", nil); err == nil {
		t.Fatal("configuration with a nonexistent interface succeeded")
	}
	if netxEnabled() {
		t.Fatal("failed configuration left binding enabled")
	}

	loopback := netxTestLoopback()
	if loopback == "" {
		return
	}
	if err := netxConfigure(loopback, nil); err != nil {
		t.Fatalf("configure %s: %v", loopback, err)
	}
	if err := netxConfigure("netx-no-such-interface0", nil); err == nil {
		t.Fatal("configuration with a nonexistent interface succeeded")
	}
	if !netxEnabled() || netxInterfaceName() != loopback {
		t.Fatalf("failed configuration changed existing binding: enabled=%v iface=%q", netxEnabled(), netxInterfaceName())
	}
}

func TestNetxAutoDetectFailureKeepsConfig(t *testing.T) {
	netxTestReset(t)
	loopback := netxTestLoopback()
	if loopback == "" {
		t.Skipf("interface binding is not supported on %s", runtime.GOOS)
	}
	if err := netxConfigure(loopback, nil); err != nil {
		t.Fatalf("configure %s: %v", loopback, err)
	}
	orig := netxAutoDetect
	netxAutoDetect = func() (string, error) {
		return "", errors.New("test: no default route")
	}
	defer func() { netxAutoDetect = orig }()

	err := netxConfigure("auto", nil)
	if err == nil {
		t.Fatal("auto detection without a default route succeeded")
	}
	if !strings.Contains(err.Error(), "no default route") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !netxEnabled() || netxInterfaceName() != loopback {
		t.Fatalf("failed auto detection cleared binding: enabled=%v iface=%q", netxEnabled(), netxInterfaceName())
	}
	if netxResolver() == nil {
		t.Fatal("failed auto detection cleared DNS resolver")
	}
}

func TestNetxDialerUsesPinnedResolver(t *testing.T) {
	netxTestReset(t)

	plain := netxDialer(0)
	if plain.Resolver != nil || plain.Control != nil {
		t.Fatalf("disabled dialer has resolver=%v control=%v", plain.Resolver != nil, plain.Control != nil)
	}

	loopback := netxTestLoopback()
	if loopback == "" {
		t.Skipf("interface binding is not supported on %s", runtime.GOOS)
	}
	if err := netxConfigure(loopback, nil); err != nil {
		t.Fatalf("configure %s: %v", loopback, err)
	}
	want := netxResolver()
	if want == nil {
		t.Fatal("configured state has no pinned resolver")
	}
	dialer := netxDialer(3 * time.Second)
	if dialer.Resolver != want {
		t.Fatalf("dialer resolver = %p, want pinned resolver %p", dialer.Resolver, want)
	}
	if dialer.Control == nil {
		t.Fatal("dialer control is not set")
	}
	if dialer.Timeout != 3*time.Second {
		t.Fatalf("dialer timeout = %v, want 3s", dialer.Timeout)
	}
}

func TestNetxConfigureDNS(t *testing.T) {
	netxTestReset(t)
	loopback := netxTestLoopback()
	if loopback == "" {
		t.Skipf("interface binding is not supported on %s", runtime.GOOS)
	}
	if err := netxConfigure(loopback, []string{"1.1.1.1", "8.8.8.8:53", "2001:db8::1"}); err != nil {
		t.Fatalf("configure valid DNS servers: %v", err)
	}
	// A server hostname would force the pinned resolver to resolve itself.
	if err := netxConfigure(loopback, []string{"dns.example.com:53"}); err == nil {
		t.Fatal("DNS server hostname was accepted")
	}
	if !netxEnabled() || netxInterfaceName() != loopback {
		t.Fatalf("failed DNS configuration changed binding: enabled=%v iface=%q", netxEnabled(), netxInterfaceName())
	}
	if netxResolver() == nil {
		t.Fatal("failed DNS configuration cleared the resolver")
	}
}

func TestNetxDisabledDefaults(t *testing.T) {
	netxTestReset(t)
	if netxEnabled() {
		t.Fatal("binding enabled after disabling")
	}
	if netxInterfaceName() != "" {
		t.Fatalf("interface name = %q, want empty", netxInterfaceName())
	}
	if netxResolver() != nil {
		t.Fatal("pinned resolver present while disabled")
	}
	ips, err := netxResolve(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatalf("resolve loopback: %v", err)
	}
	if len(ips) == 0 || ips[0] != "127.0.0.1" {
		t.Fatalf("unexpected resolved addresses: %v", ips)
	}
	transport := netxTransport()
	if transport == nil || transport.DialContext == nil {
		t.Fatal("transport has no DialContext")
	}
	ln, err := netxListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	if err := ln.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
		done <- err
	}()
	conn, err := netxDialContext(context.Background(), "tcp4", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
	if err := <-done; err != nil {
		t.Fatalf("accept: %v", err)
	}

	// The stock TCP hole-punching path still enables address/port reuse.
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen hole punch target: %v", err)
	}
	defer server.Close()
	go func() {
		conn, err := server.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	punchConn, err := netxDialTCPString("tcp4", "127.0.0.1:0", server.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial with reuse (binding disabled): %v", err)
	}
	punchConn.Close()
}
