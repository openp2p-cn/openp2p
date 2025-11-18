package openp2p

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

type v4Listener struct {
	conns       sync.Map
	port        int
	acceptCh    chan bool
	running     bool
	tcpListener *net.TCPListener
	udpListener quic.Listener
	wg          sync.WaitGroup
}

func (vl *v4Listener) start() {
	vl.running = true
	v4l.acceptCh = make(chan bool, 500)
	vl.wg.Add(1)
	go func() {
		defer vl.wg.Done()
		for vl.running {
			vl.listenTCP()
			time.Sleep(UnderlayTCPConnectTimeout)
		}
	}()
	vl.wg.Add(1)
	go func() {
		defer vl.wg.Done()
		for vl.running {
			vl.listenUDP()
			time.Sleep(UnderlayTCPConnectTimeout)
		}
	}()
}

func (vl *v4Listener) stop() {
	vl.running = false
	if vl.tcpListener != nil {
		vl.tcpListener.Close()
	}
	if vl.udpListener != nil {
		vl.udpListener.Close()
	}
	vl.wg.Wait()
}

func (vl *v4Listener) listenTCP() error {
	gLog.d("v4Listener listenTCP %d start", vl.port)
	defer gLog.d("v4Listener listenTCP %d end", vl.port)
	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("0.0.0.0:%d", vl.port)) // system will auto listen both v4 and v6
	var err error
	vl.tcpListener, err = net.ListenTCP("tcp", addr)
	if err != nil {
		gLog.e("v4Listener listen %d error:", vl.port, err)
		return err
	}
	defer vl.tcpListener.Close()
	for {
		c, err := vl.tcpListener.Accept()
		if err != nil {
			break
		}
		utcp := &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c, connectTime: time.Now()}
		go vl.handleConnection(utcp)
	}
	vl.tcpListener = nil
	return nil
}

func (vl *v4Listener) listenUDP() error {
	gLog.d("v4Listener listenUDP %d start", vl.port)
	defer gLog.d("v4Listener listenUDP %d end", vl.port)
	var err error
	vl.udpListener, err = quic.ListenAddr(fmt.Sprintf("0.0.0.0:%d", vl.port), generateTLSConfig(),
		&quic.Config{Versions: quicVersion, MaxIdleTimeout: TunnelIdleTimeout, DisablePathMTUDiscovery: true})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), UnderlayConnectTimeout)
	defer cancel()
	defer vl.udpListener.Close()
	for {
		sess, err := vl.udpListener.Accept(context.Background())
		if err != nil {
			break
		}
		stream, err := sess.AcceptStream(ctx)
		if err != nil {
			break
		}
		ul := &underlayQUIC{writeMtx: &sync.Mutex{}, Stream: stream, Connection: sess}
		go vl.handleConnection(ul)
	}
	vl.udpListener = nil
	return err
}

func (vl *v4Listener) handleConnection(ul underlay) {
	gLog.d("v4Listener accept connection: %s", ul.RemoteAddr().String())
	ul.SetReadDeadline(time.Now().Add(UnderlayTCPConnectTimeout))
	_, buff, err := ul.ReadBuffer()
	if err != nil || buff == nil {
		gLog.e("v4Listener read MsgTunnelHandshake error:%s", err)
	}
	ul.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, buff)
	var tid uint64
	if string(buff) == "OpenP2P,hello" { // old client
		// save remoteIP as key
		remoteAddr := ul.RemoteAddr().(*net.TCPAddr).IP
		ipBytes := remoteAddr.To4()
		tid = uint64(binary.BigEndian.Uint32(ipBytes)) // bytes not enough for uint64
		gLog.d("hello %s", string(buff))
	} else {
		if len(buff) < 8 {
			return
		}
		tid = binary.LittleEndian.Uint64(buff[:8])
		gLog.d("hello %d", tid)
	}
	// clear timeout connection
	vl.conns.Range(func(idx, i interface{}) bool {
		ut := i.(*underlayTCP)
		if ut.connectTime.Before(time.Now().Add(-UnderlayTCPConnectTimeout)) {
			vl.conns.Delete(idx)
		}
		return true
	})
	vl.conns.Store(tid, ul)
	select {
	case vl.acceptCh <- true:
	default:
		gLog.e("msgQueue full, drop it")
	}
}

func (vl *v4Listener) getUnderlay(tid uint64) underlay {
	for i := 0; i < 100; i++ {
		select {
		case <-time.After(time.Millisecond * 50):
		case <-vl.acceptCh:
		}
		if u, ok := vl.conns.LoadAndDelete(tid); ok {
			return u.(underlay)
		}
	}
	return nil
}
