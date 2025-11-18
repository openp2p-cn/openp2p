package openp2p

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	reuse "github.com/openp2p-cn/go-reuseport"
)

type underlayTCP struct {
	writeMtx *sync.Mutex
	net.Conn
	connectTime time.Time
}

func (conn *underlayTCP) Protocol() string {
	return "tcp"
}

func (conn *underlayTCP) ReadBuffer() (*openP2PHeader, []byte, error) {
	return DefaultReadBuffer(conn)
}

func (conn *underlayTCP) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	return DefaultWriteBytes(conn, mainType, subType, data)
}

func (conn *underlayTCP) WriteBuffer(data []byte) error {
	return DefaultWriteBuffer(conn, data)
}

func (conn *underlayTCP) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	return DefaultWriteMessage(conn, mainType, subType, packet)
}

func (conn *underlayTCP) Close() error {
	return conn.Conn.Close()
}
func (conn *underlayTCP) WLock() {
	conn.writeMtx.Lock()
}
func (conn *underlayTCP) WUnlock() {
	conn.writeMtx.Unlock()
}

func listenTCP(host string, port int, localPort int, mode string, t *P2PTunnel) (underlay, error) {
	if mode == LinkModeTCPPunch || mode == LinkModeTCP6 {
		if compareVersion(t.config.peerVersion, SyncServerTimeVersion) < 0 {
			gLog.d("peer version %s less than %s", t.config.peerVersion, SyncServerTimeVersion)
		} else {
			ts := time.Duration(int64(t.punchTs) + GNetwork.dt - time.Now().UnixNano())
			gLog.d("sleep %d ms", ts/time.Millisecond)
			time.Sleep(ts)
		}
		// gLog.d(" send tcp punch: ", fmt.Sprintf("0.0.0.0:%d", localPort), "-->", fmt.Sprintf("%s:%d", host, port))
		var c net.Conn
		var err error
		if mode == LinkModeTCPPunch {
			c, err = reuse.DialTimeout("tcp", fmt.Sprintf("0.0.0.0:%d", localPort), fmt.Sprintf("%s:%d", host, port), CheckActiveTimeout)
		} else {
			c, err = reuse.DialTimeout("tcp6", fmt.Sprintf("[::]:%d", localPort), fmt.Sprintf("[%s]:%d", t.config.peerIPv6, port), CheckActiveTimeout)
		}
		if err != nil {
			// gLog.d("send tcp punch: ", err)
			return nil, err
		}
		utcp := &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}
		_, buff, err := utcp.ReadBuffer()
		if err != nil {
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.d("handshake flag:%s", string(buff))
		}
		utcp.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, buff)
		return utcp, nil
	}
	GNetwork.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
	tid := t.id
	if compareVersion(t.config.peerVersion, PublicIPVersion) < 0 { // old version
		ipBytes := net.ParseIP(t.config.peerIP).To4()
		tid = uint64(binary.BigEndian.Uint32(ipBytes))
		gLog.d("compatible with old client, use ip as key:%d", tid)
	}
	var ul underlay
	if v4l != nil {
		ul = v4l.getUnderlay(tid)
	}
	if ul == nil {
		return nil, ErrConnectPublicV4
	}
	return ul, nil
}

func dialTCP(host string, port int, localPort int, mode string) (*underlayTCP, error) {
	var c net.Conn
	var err error
	network := "tcp"
	localAddr := fmt.Sprintf("0.0.0.0:%d", localPort)
	remoteAddr := fmt.Sprintf("%s:%d", host, port)
	if mode == LinkModeTCP6 { // address need [ip]
		network = "tcp6"
		localAddr = fmt.Sprintf("[::]:%d", localPort)
		remoteAddr = fmt.Sprintf("[%s]:%d", host, port)
	}
	if mode == LinkModeTCP4 || mode == LinkModeIntranet { // random port
		localAddr = fmt.Sprintf("0.0.0.0:%d", 0)
	}
	gLog.dev("send tcp punch: %s --> %s", localAddr, remoteAddr)

	c, err = reuse.DialTimeout(network, localAddr, remoteAddr, CheckActiveTimeout)
	if err != nil {
		gLog.dev("send tcp punch: %v", err)
	}

	if err != nil {
		gLog.dev("Dial %s:%d error:%s", host, port, err)
		return nil, err
	}
	tc := c.(*net.TCPConn)
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(UnderlayTCPKeepalive)
	gLog.d("Dial %s:%d OK", host, port)
	return &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}, nil
}
