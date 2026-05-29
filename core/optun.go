package openp2p

import (
	"github.com/openp2p-cn/wireguard-go/tun"
)

const optunMTU = 1420

var AndroidSDWANConfig chan []byte
var preAndroidSDWANConfig string

type optun struct {
	tunName string
	dev     tun.Device
}

func (t *optun) Stop() error {
	t.dev.Close()
	return nil
}
func init() {
	AndroidSDWANConfig = make(chan []byte, 1)
}
