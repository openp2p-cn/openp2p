package openp2p

import (
	"time"
)

type underlay interface {
	ReadBuffer() (*openP2PHeader, []byte, error)
	WriteBytes(uint16, uint16, []byte) error
	WriteBuffer([]byte) error
	WriteMessage(uint16, uint16, interface{}) error
	Close() error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Protocol() string
}
