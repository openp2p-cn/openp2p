package openp2p

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/openp2p-cn/wireguard-go/tun"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const (
	tunIfaceName = "optun"
	PIHeaderSize = 0
)

func (t *optun) Start(localAddr string, detail *SDWANInfo) error {
	// check wintun.dll
	tmpFile := filepath.Dir(os.Args[0]) + "/wintun.dll"
	fs, err := os.Stat(tmpFile)
	if err != nil || fs.Size() == 0 {
		url := fmt.Sprintf("https://openp2p.cn/download/v1/latest/wintun/%s/wintun.dll", runtime.GOARCH)
		err = downloadFile(url, "", tmpFile)
		if err != nil {
			os.Remove(tmpFile)
			return err
		}
	}

	t.tunName = tunIfaceName

	uuid := &windows.GUID{
		Data1: 0xf411e821,
		Data2: 0xb310,
		Data3: 0x4567,
		Data4: [8]byte{0x80, 0x42, 0x83, 0x7e, 0xf4, 0x56, 0xce, 0x13},
	}
	t.dev, err = tun.CreateTUNWithRequestedGUID(t.tunName, uuid, 1420)
	if err != nil { // retry
		t.dev, err = tun.CreateTUNWithRequestedGUID(t.tunName, uuid, 1420)
	}

	if err != nil {
		return err
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
	nativeTunDevice := wintun.(*tun.NativeTun)
	link := winipcfg.LUID(nativeTunDevice.LUID())
	ip, err := netip.ParsePrefix(localAddr)
	if err != nil {
		gLog.Printf(LvERROR, "ParsePrefix error:%s, luid:%d,localAddr:%s", err, nativeTunDevice.LUID(), localAddr)
		return err
	}
	err = link.SetIPAddresses([]netip.Prefix{ip})
	if err != nil {
		gLog.Printf(LvERROR, "SetIPAddresses error:%s, netip.Prefix:%+v", err, []netip.Prefix{ip})
		return err
	}
	return nil
}

func addRoute(dst, gw, ifname string) error {
	_, dstNet, err := net.ParseCIDR(dst)
	if err != nil {
		return err
	}
	i, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	params := make([]string, 0)
	params = append(params, "add")
	params = append(params, dstNet.IP.String())
	params = append(params, "mask")
	params = append(params, net.IP(dstNet.Mask).String())
	params = append(params, gw)
	params = append(params, "if")
	params = append(params, strconv.Itoa(i.Index))
	// gLogger.Println(LevelINFO, "windows add route params:", params)
	execCommand("route", true, params...)
	return nil
}

func delRoute(dst, gw string) error {
	_, dstNet, err := net.ParseCIDR(dst)
	if err != nil {
		return err
	}
	params := make([]string, 0)
	params = append(params, "delete")
	params = append(params, dstNet.IP.String())
	params = append(params, "mask")
	params = append(params, net.IP(dstNet.Mask).String())
	params = append(params, gw)
	// gLogger.Println(LevelINFO, "windows delete route params:", params)
	execCommand("route", true, params...)
	return nil
}

func delRoutesByGateway(gateway string) error {
	cmd := exec.Command("route", "print", "-4")
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
		if len(fields) >= 5 {
			cmd := exec.Command("route", "delete", fields[0], "mask", fields[1], gateway)
			err := cmd.Run()
			if err != nil {
				fmt.Println("Delete route error:", err)
			}
			fmt.Printf("Delete route ok: %s %s %s\n", fields[0], fields[1], gateway)
		}
	}
	return nil
}
