package openp2p

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	reuse "github.com/openp2p-cn/go-reuseport"
)

type underlayTCP struct {
	writeMtx *sync.Mutex
	net.Conn
}

func (conn *underlayTCP) Protocol() string {
	return "tcp"
}

func (conn *underlayTCP) ReadBuffer() (*openP2PHeader, []byte, error) {
	headBuf := make([]byte, openP2PHeaderSize)
	_, err := io.ReadFull(conn, headBuf)
	if err != nil {
		return nil, nil, err
	}
	head, err := decodeHeader(headBuf)
	if err != nil {
		return nil, nil, err
	}
	dataBuf := make([]byte, head.DataLen)
	_, err = io.ReadFull(conn, dataBuf)
	return head, dataBuf, err
}

func (conn *underlayTCP) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	conn.writeMtx.Lock()
	_, err := conn.Write(writeBytes)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP) WriteBuffer(data []byte) error {
	conn.writeMtx.Lock()
	_, err := conn.Write(data)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	// TODO: call newMessage
	data, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	conn.writeMtx.Lock()
	_, err = conn.Write(writeBytes)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP) Close() error {
	return conn.Conn.Close()
}

func listenTCP(host string, port int, localPort int, mode string, t *P2PTunnel) (*underlayTCP, error) {
	if mode == LinkModeTCPPunch {
		if compareVersion(t.config.peerVersion, SyncServerTimeVersion) == LESS {
			gLog.Printf(LvDEBUG, "peer version %s less than %s", t.config.peerVersion, SyncServerTimeVersion)
		} else {
			ts := time.Duration(int64(t.punchTs) + t.pn.dt - time.Now().UnixNano())
			gLog.Printf(LvDEBUG, "sleep %d ms", ts/time.Millisecond)
			time.Sleep(ts)
		}
		gLog.Println(LvDEBUG, (time.Now().UnixNano()-t.pn.dt)/(int64)(time.Millisecond), " send tcp punch: ", fmt.Sprintf("0.0.0.0:%d", localPort), "-->", fmt.Sprintf("%s:%d", host, port))
		c, err := reuse.DialTimeout("tcp", fmt.Sprintf("0.0.0.0:%d", localPort), fmt.Sprintf("%s:%d", host, port), CheckActiveTimeout)
		if err != nil {
			gLog.Println(LvDEBUG, "send tcp punch: ", err)
			return nil, err
		}
		return &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}, nil
	}
	t.pn.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("0.0.0.0:%d", localPort))
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	l.SetDeadline(time.Now().Add(CheckActiveTimeout))
	c, err := l.Accept()
	defer l.Close()
	if err != nil {
		return nil, err
	}
	return &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}, nil
}

func dialTCP(host string, port int, localPort int, mode string) (*underlayTCP, error) {
	var c net.Conn
	var err error
	if mode == LinkModeTCPPunch {
		c, err = reuse.DialTimeout("tcp", fmt.Sprintf("0.0.0.0:%d", localPort), fmt.Sprintf("%s:%d", host, port), CheckActiveTimeout)
	} else {
		c, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), CheckActiveTimeout)
	}

	if err != nil {
		gLog.Printf(LvERROR, "Dial %s:%d error:%s", host, port, err)
		return nil, err
	}
	gLog.Printf(LvDEBUG, "Dial %s:%d OK", host, port)
	return &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}, nil
}
