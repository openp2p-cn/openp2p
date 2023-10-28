package openp2p

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

type v4Listener struct {
	conns    sync.Map
	port     int
	acceptCh chan bool
}

func (vl *v4Listener) start() error {
	v4l.acceptCh = make(chan bool, 10)
	for {
		vl.listen()
		time.Sleep(time.Second * 5)
	}
}

func (vl *v4Listener) listen() error {
	gLog.Printf(LvINFO, "listen %d start", vl.port)
	defer gLog.Printf(LvINFO, "listen %d end", vl.port)
	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("0.0.0.0:%d", vl.port))
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		gLog.Printf(LvERROR, "listen %d error:", vl.port, err)
		return err
	}
	defer l.Close()
	for {
		c, err := l.Accept()
		if err != nil {
			break
		}
		go vl.handleConnection(c)
	}
	return nil
}
func (vl *v4Listener) handleConnection(c net.Conn) {
	gLog.Println(LvDEBUG, "v4Listener accept connection: ", c.RemoteAddr().String())
	utcp := &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}
	utcp.SetReadDeadline(time.Now().Add(time.Second * 5))
	_, buff, err := utcp.ReadBuffer()
	if err != nil {
		gLog.Printf(LvERROR, "utcp.ReadBuffer error:", err)
	}
	utcp.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, buff)
	var tid uint64
	if string(buff) == "OpenP2P,hello" { // old client
		// save remoteIP as key
		remoteAddr := c.RemoteAddr().(*net.TCPAddr).IP
		ipBytes := remoteAddr.To4()
		tid = uint64(binary.BigEndian.Uint32(ipBytes)) // bytes not enough for uint64
		gLog.Println(LvDEBUG, "hello ", string(buff))
	} else {
		if len(buff) < 8 {
			return
		}
		tid = binary.LittleEndian.Uint64(buff[:8])
		gLog.Println(LvDEBUG, "hello ", tid)
	}
	vl.conns.Store(tid, utcp)
	vl.acceptCh <- true
}

func (vl *v4Listener) getUnderlayTCP(tid uint64) *underlayTCP {
	for i := 0; i < 100; i++ {
		select {
		case <-time.After(time.Millisecond * 50):
		case <-vl.acceptCh:
		}
		if u, ok := vl.conns.LoadAndDelete(tid); ok {
			return u.(*underlayTCP)
		}
	}
	return nil
}
