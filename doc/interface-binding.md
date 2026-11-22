# Binding underlay sockets to a network interface

## Problem

On a host that runs a transparent proxy / TUN (mihomo, Clash, sing-box, ...),
all traffic of openp2p follows the default route into the TUN device:

- `api.openp2p.cn` resolves to a fake-IP such as `198.18.0.22`;
- TCP/UDP/QUIC underlay sockets get `198.18.0.1` as their source address;
- hole punching and the public listeners (port forwarding / public IP test)
  either go through the proxy or bind to the TUN, so direct P2P fails.

openp2p had no way to say "use the physical interface for the underlay".
Global OS route changes are not acceptable for a daemon, and naming a
physical interface is enough to fix this on the host.

## Usage

```
openp2p -interface en0
openp2p -interface auto -dns 223.5.5.5,119.29.29.29
```

- `-interface <name>`: bind underlay sockets to this interface (Darwin and
  Linux). An explicit name always wins over auto-detection.
- `-interface auto`: pick the interface of the IPv4 default route. Common
  virtual prefixes are skipped (`utun*`, `tun*`, `tap*`, `lo*`, `Meta*`,
  `wg*`, `tailscale*`, `sing*`, `bridge*`, `vmnet*`, ...). If the proxy TUN
  uses another name, use an explicit interface name.
- `-dns <ip[:port],...>`: DNS servers used while an interface is bound.
  Default: `119.29.29.29:53,223.5.5.5:53`. Only IP addresses are accepted;
  a server hostname would need the resolver itself.
- Environment: `OPENP2P_IFACE`, `OPENP2P_DNS`.
- `config.json`: `"network": {"Interface": "en0", "DNS": ["223.5.5.5:53"]}`.
- Precedence: flag > environment > `config.json`. The merged value is saved
  by the existing config save, so it is sticky like `-node` / `OPENP2P_TOKEN`.

With no interface configured (the default) openp2p behaves exactly as
before: no socket control callback, no pinned resolver.

## What is bound

When an interface is configured:

- WebSocket/TLS to the server, NAT detection, hole punching, underlay
  TCP/TCP6/QUIC, update/download HTTP clients and DNS dials are bound.
- DNS uses a Go resolver (`PreferGo`) that dials the pinned servers through
  the bound interface. A resolved server address in `198.18.0.0/15` is
  rejected instead of being dialed.
- Public listeners (`v4Listener` TCP/QUIC) are bound as well; accepted TCP
  connections are bound to the same interface.
- Local application listeners, overlay/p2p tunnels and ping are untouched.

Platform mechanisms:

| Platform | Mechanism |
| --- | --- |
| Darwin | `IP_BOUND_IF` / `IPV6_BOUND_IF` on the socket |
| Linux | `SO_BINDTODEVICE` (usually requires root) |
| other | configuring an interface fails with a clear error |

## Features disabled while bound

A system TUN and these features cannot coexist safely, so they are skipped
with one log line each:

- UPnP / NAT-PMP port mapping (would program the router for the wrong port);
- SDWAN (it starts its own TUN);
- IPv6 refresh/reporting (it is fetched over the default route; the reported
  address would belong to the TUN).

NAT type detection still runs, so the reported NAT type may be less
accurate (for example `2`) even though the interface is bound. Do not use
NAT type as a health check when binding is enabled.

## mihomo TUN example

```bash
# physical interface: en0 (macOS) / e.g. eth0 or enp3s0 (Linux)
openp2p -token "$TOKEN" -node my-node \
  -interface en0 -dns 223.5.5.5,119.29.29.29
```

Verify before/after with `lsof`:

```bash
route -n get 1.1.1.1          # illustrates the TUN, e.g. utun1024 -> 198.18.0.1
pid=$(pgrep -f "openp2p.*-token")
lsof -nP -a -p "$pid" -i
```

Without binding the server connection is
`198.18.0.1:... -> 198.18.0.x:27183`; with binding it is
`192.0.2.10:... -> <server real IP>:27183`.

## Limitations

- The bound public listeners only accept traffic on the bound interface.
  A local health check against `127.0.0.1:<PublicIPPort>` is filtered;
  probe the LAN or public address instead.
- Linux `SO_BINDTODEVICE` needs root or `CAP_NET_RAW`.
- Windows and other platforms are not supported; `-interface` fails
  immediately with an explicit error so a misconfigured service does not
  silently run unbound.
- Binding fixes openp2p only; other processes on the host still use the TUN.
- `-interface auto` skips common virtual prefixes; it cannot recognize every
  VPN naming scheme, so prefer an explicit name when in doubt.
- IPv6 underlay refresh is disabled while bound, so IPv6 direct connectivity
  may be unavailable even if the host has a global IPv6 address.

## Compatibility and test plan

- Default (no `-interface`, no environment variable, no config field): stock
  code paths, no `Control` callback, no pinned resolver.
- `go test ./core -run TestNetx -count=3` covers loopback TCP/UDP/QUIC-style
  listeners, accepted-connection binding, local port reuse, fake-IP
  rejection, auto detection and invalid configuration.
- Real machine check with a TUN active: `lsof` must show zero `198.18.*`
  endpoints and only physical-interface sources; the log must show the real
  resolved server IP (`resolved <host> -> [<real IP>]`).
