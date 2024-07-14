package openp2p

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type underlayTCP6 struct {
	writeMtx *sync.Mutex
	net.Conn
}

func (conn *underlayTCP6) Protocol() string {
	return "tcp6"
}

func (conn *underlayTCP6) ReadBuffer() (*openP2PHeader, []byte, error) {
	return DefaultReadBuffer(conn)
}

func (conn *underlayTCP6) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	return DefaultWriteBytes(conn, mainType, subType, data)
}

func (conn *underlayTCP6) WriteBuffer(data []byte) error {
	return DefaultWriteBuffer(conn, data)
}

func (conn *underlayTCP6) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	return DefaultWriteMessage(conn, mainType, subType, packet)
}

func (conn *underlayTCP6) Close() error {
	return conn.Conn.Close()
}
func (conn *underlayTCP6) WLock() {
	conn.writeMtx.Lock()
}
func (conn *underlayTCP6) WUnlock() {
	conn.writeMtx.Unlock()
}
func listenTCP6(port int, timeout time.Duration) (*underlayTCP6, error) {
	addr, _ := net.ResolveTCPAddr("tcp6", fmt.Sprintf("[::]:%d", port))
	l, err := net.ListenTCP("tcp6", addr)
	if err != nil {
		return nil, err
	}
	defer l.Close()
	l.SetDeadline(time.Now().Add(timeout))
	c, err := l.Accept()
	defer l.Close()
	if err != nil {
		return nil, err
	}
	return &underlayTCP6{writeMtx: &sync.Mutex{}, Conn: c}, nil
}

func dialTCP6(host string, port int) (*underlayTCP6, error) {
	c, err := net.DialTimeout("tcp6", fmt.Sprintf("[%s]:%d", host, port), UnderlayConnectTimeout)
	if err != nil {
		gLog.Printf(LvERROR, "Dial %s:%d error:%s", host, port, err)
		return nil, err
	}
	return &underlayTCP6{writeMtx: &sync.Mutex{}, Conn: c}, nil
}
