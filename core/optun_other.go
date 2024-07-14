//go:build !linux && !windows && !darwin
// +build !linux,!windows,!darwin

package openp2p

import "github.com/openp2p-cn/wireguard-go/tun"

const (
	tunIfaceName = "optun"
	PIHeaderSize = 0
)

func (t *optun) Start(localAddr string, detail *SDWANInfo) error {
	var err error
	t.tunName = tunIfaceName
	t.dev, err = tun.CreateTUN(t.tunName, 1420)

	if err != nil {
		return err
	}
	err = setTunAddr(t.tunName, localAddr, detail.Gateway, t.dev)
	if err != nil {
		return err
	}

	return nil
}

func addRoute(dst, gw, ifname string) error {
	return nil
}

func delRoute(dst, gw string) error {
	return nil
}
func addTunAddr(localAddr, remoteAddr string) error {
	return nil
}

func delTunAddr(localAddr, remoteAddr string) error {
	return nil
}
