package openp2p

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/xtaci/kcp-go/v5"
)

type underlayKCP struct {
	listener *kcp.Listener
	writeMtx *sync.Mutex
	*kcp.UDPSession
}

func (conn *underlayKCP) Protocol() string {
	return "kcp"
}

func (conn *underlayKCP) ReadBuffer() (*openP2PHeader, []byte, error) {
	return DefaultReadBuffer(conn)
}

func (conn *underlayKCP) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	return DefaultWriteBytes(conn, mainType, subType, data)
}

func (conn *underlayKCP) WriteBuffer(data []byte) error {
	return DefaultWriteBuffer(conn, data)
}

func (conn *underlayKCP) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	return DefaultWriteMessage(conn, mainType, subType, packet)
}

func (conn *underlayKCP) Close() error {
	conn.UDPSession.Close()
	return nil
}
func (conn *underlayKCP) WLock() {
	conn.writeMtx.Lock()
}
func (conn *underlayKCP) WUnlock() {
	conn.writeMtx.Unlock()
}
func (conn *underlayKCP) CloseListener() {
	if conn.listener != nil {
		conn.listener.Close()
	}
}

func (conn *underlayKCP) Accept() error {
	kConn, err := conn.listener.AcceptKCP()
	if err != nil {
		conn.listener.Close()
		return err
	}
	kConn.SetNoDelay(0, 40, 0, 0)
	kConn.SetWindowSize(512, 512)
	kConn.SetWriteBuffer(1024 * 128)
	kConn.SetReadBuffer(1024 * 128)
	conn.UDPSession = kConn
	return nil
}

func listenKCP(addr string, idleTimeout time.Duration) (*underlayKCP, error) {
	gLog.d("kcp listen on %s", addr)
	listener, err := kcp.ListenWithOptions(addr, nil, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("quic.ListenAddr error:%s", err)
	}
	ul := &underlayKCP{listener: listener, writeMtx: &sync.Mutex{}}
	err = ul.Accept()
	if err != nil {
		ul.CloseListener()
		return nil, fmt.Errorf("accept KCP error:%s", err)
	}
	return ul, nil
}

func dialKCP(conn *net.UDPConn, remoteAddr *net.UDPAddr, idleTimeout time.Duration) (*underlayKCP, error) {
	conn.SetDeadline(time.Now().Add(idleTimeout))
	kConn, err := kcp.NewConn(remoteAddr.String(), nil, 0, 0, conn)
	if err != nil {
		return nil, fmt.Errorf("quic.DialContext error:%s", err)
	}
	kConn.SetNoDelay(0, 40, 0, 0)
	kConn.SetWindowSize(512, 512)
	kConn.SetWriteBuffer(1024 * 128)
	kConn.SetReadBuffer(1024 * 128)
	ul := &underlayKCP{nil, &sync.Mutex{}, kConn}
	return ul, nil
}
