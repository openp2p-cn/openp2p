package main

import (
	"time"
)

type p2pConn interface {
	ReadMessage() (*openP2PHeader, []byte, error)
	WriteBytes(uint16, uint16, []byte) error
	WriteBuffer([]byte) error
	WriteMessage(uint16, uint16, interface{}) error
	Close() error
	Accept() error
	CloseListener()
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}
