//go:build !android
// +build !android

// optun_linux.go
package openp2p

import (
	"fmt"
	"net"
	"os"

	"github.com/openp2p-cn/wireguard-go/tun"
	"github.com/vishvananda/netlink"
)

const (
	tunIfaceName = "optun"
	PIHeaderSize = 0
	// sdwan
	ReadTunBuffSize = 2048
	ReadTunBuffNum  = 16
)

var previousIP = ""

func (t *optun) Start(localAddr string, detail *SDWANInfo) error {
	var err error
	t.tunName = tunIfaceName
	t.dev, err = tun.CreateTUN(t.tunName, int(detail.Mtu))
	if err != nil {
		return err
	}
	err = os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)
	if err != nil {
		gLog.e("write ip_forward error:%s", err)
	}
	return nil
}

func (t *optun) Read(bufs [][]byte, sizes []int, offset int) (n int, err error) {
	return t.dev.Read(bufs, sizes, offset)
}

func (t *optun) Write(bufs [][]byte, offset int) (int, error) {
	return t.dev.Write(bufs, offset)
}

func setTunAddr(ifname, localAddr, remoteAddr string, wintun interface{}) error {
	ifce, err := netlink.LinkByName(ifname)
	if err != nil {
		return err
	}
	netlink.LinkSetMTU(ifce, int(gConf.getSDWAN().Mtu))
	netlink.LinkSetTxQLen(ifce, 1000)
	netlink.LinkSetUp(ifce)

	ln, err := netlink.ParseIPNet(localAddr)
	if err != nil {
		return err
	}
	ln.Mask = net.CIDRMask(32, 32)
	rn, err := netlink.ParseIPNet(remoteAddr)
	if err != nil {
		return err
	}
	rn.Mask = net.CIDRMask(32, 32)

	addr := &netlink.Addr{
		IPNet: ln,
		Peer:  rn,
	}
	if previousIP != "" {
		lnDel, err := netlink.ParseIPNet(previousIP)
		if err != nil {
			return err
		}
		lnDel.Mask = net.CIDRMask(32, 32)

		addrDel := &netlink.Addr{
			IPNet: lnDel,
			Peer:  rn,
		}
		netlink.AddrDel(ifce, addrDel)
	}
	previousIP = localAddr
	return netlink.AddrAdd(ifce, addr)
}

func addRoute(dst, gw, ifname string) error {
	_, networkid, err := net.ParseCIDR(dst)
	if err != nil {
		return err
	}
	ipGW := net.ParseIP(gw)
	if ipGW == nil {
		return fmt.Errorf("parse gateway %s failed", gw)
	}
	route := &netlink.Route{
		Dst: networkid,
		Gw:  ipGW,
	}
	return netlink.RouteAdd(route)
}

func delRoute(dst, gw string) error {
	_, networkid, err := net.ParseCIDR(dst)
	if err != nil {
		return err
	}
	route := &netlink.Route{
		Dst: networkid,
	}
	return netlink.RouteDel(route)
}

func delRoutesByGateway(gateway string) error {
	ipGW := net.ParseIP(gateway)
	if ipGW == nil {
		return fmt.Errorf("invalid gateway IP: %s", gateway)
	}

	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list routes: %v", err)
	}

	for _, route := range routes {
		if route.Gw != nil && route.Gw.Equal(ipGW) || (route.Dst != nil && route.Dst.IP.Equal(ipGW)) {
			err := netlink.RouteDel(&route)
			if err != nil {
				gLog.e("Failed to delete route: %v, error: %v", route, err)
				continue
			}
			gLog.i("Deleted route: %v", route)
		}
	}
	return nil
}
