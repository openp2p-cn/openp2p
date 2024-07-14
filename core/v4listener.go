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
	v4l.acceptCh = make(chan bool, 500)
	for {
		vl.listen()
		time.Sleep(UnderlayTCPConnectTimeout)
	}
}

func (vl *v4Listener) listen() error {
	gLog.Printf(LvINFO, "v4Listener listen %d start", vl.port)
	defer gLog.Printf(LvINFO, "v4Listener listen %d end", vl.port)
	addr, _ := net.ResolveTCPAddr("tcp4", fmt.Sprintf("0.0.0.0:%d", vl.port))
	l, err := net.ListenTCP("tcp4", addr)
	if err != nil {
		gLog.Printf(LvERROR, "v4Listener listen %d error:", vl.port, err)
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
	utcp := &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c, connectTime: time.Now()}
	utcp.SetReadDeadline(time.Now().Add(UnderlayTCPConnectTimeout))
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
	// clear timeout connection
	vl.conns.Range(func(idx, i interface{}) bool {
		ut := i.(*underlayTCP)
		if ut.connectTime.Before(time.Now().Add(-UnderlayTCPConnectTimeout)) {
			vl.conns.Delete(idx)
		}
		return true
	})
	vl.conns.Store(tid, utcp)
	if len(vl.acceptCh) == 0 {
		vl.acceptCh <- true
	}
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
