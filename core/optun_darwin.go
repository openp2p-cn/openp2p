package openp2p

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/openp2p-cn/wireguard-go/tun"
)

const (
	tunIfaceName = "utun"
	PIHeaderSize = 4 // utun has no IFF_NO_PI
)

func (t *optun) Start(localAddr string, detail *SDWANInfo) error {
	var err error
	t.tunName = tunIfaceName
	t.dev, err = tun.CreateTUN(t.tunName, 1420)
	if err != nil {
		return err
	}
	t.tunName, _ = t.dev.Name()
	return nil
}

func (t *optun) Read(bufs [][]byte, sizes []int, offset int) (n int, err error) {
	return t.dev.Read(bufs, sizes, offset)
}

func (t *optun) Write(bufs [][]byte, offset int) (int, error) {
	return t.dev.Write(bufs, offset)
}

func setTunAddr(ifname, localAddr, remoteAddr string, wintun interface{}) error {
	li, _, err := net.ParseCIDR(localAddr)
	if err != nil {
		return fmt.Errorf("parse local addr fail:%s", err)
	}
	ri, _, err := net.ParseCIDR(remoteAddr)
	if err != nil {
		return fmt.Errorf("parse remote addr fail:%s", err)
	}
	err = exec.Command("ifconfig", ifname, "inet", li.String(), ri.String(), "up").Run()
	return err
}

func addRoute(dst, gw, ifname string) error {
	err := exec.Command("route", "add", dst, gw).Run()
	return err
}

func delRoute(dst, gw string) error {
	err := exec.Command("route", "delete", dst, gw).Run()
	return err
}
func delRoutesByGateway(gateway string) error {
	cmd := exec.Command("netstat", "-rn")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if !strings.Contains(line, gateway) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 7 && fields[0] == "default" && fields[len(fields)-1] == gateway {
			delCmd := exec.Command("route", "delete", "default", gateway)
			err := delCmd.Run()
			if err != nil {
				return err
			}
			fmt.Printf("Delete route ok: %s %s\n", "default", gateway)
		}
	}
	return nil
}
func addTunAddr(localAddr, remoteAddr string) error {
	return nil
}
func delTunAddr(localAddr, remoteAddr string) error {
	return nil
}
