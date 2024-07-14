package openp2p

import (
	"github.com/openp2p-cn/wireguard-go/tun"
)

var AndroidSDWANConfig chan []byte

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
