// optun_android.go
//go:build android
// +build android

package openp2p

import (
	"time"
)

const (
	tunIfaceName    = "optun"
	PIHeaderSize    = 0
	ReadTunBuffSize = 2048
	ReadTunBuffNum  = 16
)

var AndroidReadTun chan []byte // TODO: multi channel
var AndroidWriteTun chan []byte

func (t *optun) Start(localAddr string, detail *SDWANInfo) error {

	return nil
}

func (t *optun) Read(bufs [][]byte, sizes []int, offset int) (n int, err error) {
	bufs[0] = <-AndroidReadTun
	sizes[0] = len(bufs[0])
	return 1, nil
}

func (t *optun) Write(bufs [][]byte, offset int) (int, error) {
	AndroidWriteTun <- bufs[0]
	return len(bufs[0]), nil
}

func AndroidRead(data []byte, len int) {
	head := PacketHeader{}
	parseHeader(data, &head)
	// gLog.dev("AndroidRead tun dst ip=%s,len=%d", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String(), len)
	buf := make([]byte, len)
	copy(buf, data)
	AndroidReadTun <- buf
}

func AndroidWrite(buf []byte, timeoutMs int) int {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	select {
	case p := <-AndroidWriteTun:
		if len(p) > int(gConf.sdwan.Mtu) {
			gLog.e("AndroidWrite packet too large %d", len(p))
		}
		copy(buf, p)
		return len(p)
	case <-time.After(timeout):
		return 0
	}
}

func GetAndroidSDWANConfig(buf []byte) int {
	p := <-AndroidSDWANConfig
	copy(buf, p)
	gLog.i("AndroidSDWANConfig=%s", p)
	return len(p)
}

func GetAndroidNodeName() string {
	gLog.i("GetAndroidNodeName=%s", gConf.Network.Node)
	return gConf.Network.Node
}

func setTunAddr(ifname, localAddr, remoteAddr string, wintun interface{}) error {
	// TODO:
	return nil
}

func addRoute(dst, gw, ifname string) error {
	// TODO:
	return nil
}

func delRoute(dst, gw string) error {
	// TODO:
	return nil
}

func delRoutesByGateway(gateway string) error {
	// TODO:
	return nil
}

func init() {
	AndroidReadTun = make(chan []byte, 1000)
	AndroidWriteTun = make(chan []byte, 1000)
}
